package floatui

import (
	"math"
	"strconv"
)

type Pose struct {
	WaveY                   Tween
	previousModel           string
	modelMix                Tween
	Row                     Row
	Y, Font, Opacity, X     Tween
	Rate, RateOpacity, Flow Tween
	phase                   float64
	gone                    bool
	rateLabel               string
	rateStep                int64
	rateVisible             bool
}
type DrawRow struct {
	WaveY                            float64
	PreviousModel                    string
	ModelMix                         float64
	Row                              Row
	X, Y, Font, Opacity, RateOpacity float64
	Speed                            string
	Phase, Flow                      float64
}
type Band struct {
	Light, Visibility [33]float64
	Phase             float64
	Depth             float64
	Tone              uint8
	Points            [66]Point
	Count             int
	Opacity           float64
}
type Scene struct {
	PendingCompleted            int
	Stage                       Appearance
	Quality                     Quality
	FlowTime                    float64
	TotalRows, VisibleRows      int
	ViewportText                string
	Synthetic                   bool
	Width, Height, QuotaY       float64
	QuotaX, QuotaFont, QuotaMix float64
	PreviousQuotaText           string
	Rows                        []DrawRow
	Bands                       []Band
	Water                       [35]Point
	WaterCount                  int
	Quota                       QuotaView
	QuotaText                   string
	Transitioning               bool
}
type Animator struct {
	pendingCompleted                      int
	reducedMotion                         bool
	appearance                            appearanceController
	synthetic                             bool
	poses                                 map[string]*Pose
	order                                 []string
	layout                                Layout
	height                                Tween
	quota                                 QuotaView
	quotaLevel                            Tween
	quotaText                             string
	previousQuotaText                     string
	quotaX, quotaFont, quotaTop, quotaMix Tween
	last                                  float64
	rows                                  []DrawRow
	bands                                 []Band
	waterPhase                            float64
	flowTime                              float64
	totalRows                             int
	viewportText                          string
}

func NewAnimator() *Animator {
	return &Animator{poses: make(map[string]*Pose), layout: TargetLayout(0, 0, 600), height: settled(76), quotaLevel: settled(0), quotaText: "—", quotaX: settled(PanelWidth*.5 - 43), quotaFont: settled(31), quotaTop: settled(28), quotaMix: settled(1), rows: make([]DrawRow, 0, 32), bands: make([]Band, 0, 66)}
}

func (a *Animator) Set(snapshot Snapshot, offset int, maxHeight, now float64) {
	a.pendingCompleted = snapshot.PendingCompleted
	a.synthetic = snapshot.Mode == "synthetic"
	layout := TargetLayout(len(snapshot.Rows), offset, maxHeight)
	a.layout = layout
	wasEmpty := a.totalRows == 0
	a.totalRows = len(snapshot.Rows)
	a.viewportText = strconv.Itoa(layout.Offset+1) + "–" + strconv.Itoa(layout.Offset+layout.Visible) + "/" + strconv.Itoa(len(snapshot.Rows))
	a.height.Retarget(layout.Height, now, 0.66)
	for _, p := range a.poses {
		p.gone = true
	}
	for i := 0; i < layout.Visible; i++ {
		row := snapshot.Rows[layout.Offset+i]
		p := a.poses[row.Thread]
		y := (layout.Height-float64(layout.Visible-1)*layout.RowHeight)*.5 - layout.Font*.7 + float64(i)*layout.RowHeight
		waveY := layout.Height * (.18 + .6*float64(i+1)/float64(layout.Visible+1))
		if p == nil {
			delay := .025 * float64(i%5)
			if wasEmpty {
				delay += .13
			}
			p = &Pose{Y: Tween{From: y + 9, To: y, Start: now, Duration: 0.62}, Font: Tween{From: layout.Font * .96, To: layout.Font, Start: now, Duration: 0.78},
				Opacity: Tween{To: 1, Start: now + delay, Duration: 0.40}, X: Tween{From: 116, To: 110, Start: now + delay*.4, Duration: 0.57},
				Flow: settled(.35), rateStep: -1, phase: float64(len(a.order)) * 1.13}
			a.poses[row.Thread] = p
			p.WaveY = Tween{From: waveY + 8, To: waveY, Start: now, Duration: .72}
			p.modelMix = settled(1)
			a.order = append(a.order, row.Thread)
		} else {
			p.WaveY.Retarget(waveY, now, .72)
			p.Y.Retarget(y, now, .57)
			p.Font.Retarget(layout.Font, now, .78)
			p.Opacity.Retarget(1, now, .4)
			p.X.Retarget(110, now, .51)
		}
		if p.Row.Model != row.Model && p.Row.Model != "" {
			p.previousModel = p.Row.Model
			p.modelMix = Tween{From: 0, To: 1, Start: now, Duration: .38}
		}
		p.gone = false
		p.Row = row
		rate := row.EstimatedTokenRate
		if rate <= 0 && row.Outputting {
			rate = row.OutputActivity * .4
		}
		showRate := row.Outputting && rate > 0
		if !showRate && row.SpeedFresh && row.SpeedValid && finitePositive(row.TokenRate) {
			rate = row.TokenRate
			showRate = true
		}
		if showRate {
			if target := math.Min(rate, 999); target != p.Rate.To {
				p.Rate.Retarget(target, now, .65)
			}
		}
		if showRate != p.rateVisible {
			if showRate {
				p.RateOpacity.RetargetAfter(1, now, .055, .36)
			} else {
				p.RateOpacity.Retarget(0, now, .42)
			}
			p.rateVisible = showRate
		}
		flow := .35
		if row.Outputting {
			if rate < 12 {
				flow = .6
			} else if rate < 35 {
				flow = .9
			} else {
				flow = 1.25
			}
		}
		p.Flow.Retarget(flow, now, .7)
	}
	for _, p := range a.poses {
		if p.gone {
			p.rateVisible = false
			p.Opacity.Retarget(0, now, .44)
			p.RateOpacity.Retarget(0, now, .28)
		}
	}
	// Rapid scrolling may leave several fading pages. Keep the live page and
	// at most one page of outgoing poses; hidden sessions remain in the data engine.
	ghostBudget := max(0, MaxAnimatedRows-layout.Visible)
	kept := 0
	for _, id := range a.order {
		pose := a.poses[id]
		if pose.gone {
			if ghostBudget == 0 {
				delete(a.poses, id)
				continue
			}
			ghostBudget--
		}
		a.order[kept] = id
		kept++
	}
	a.order = a.order[:kept]
	a.quota = snapshot.Quota
	previous := a.quotaText
	if snapshot.Quota.Known {
		a.quotaLevel.Retarget(snapshot.Quota.Remaining/100, now, .65)
		a.quotaText = strconv.FormatFloat(snapshot.Quota.Remaining, 'f', 0, 64) + "%"
	} else {
		a.quotaText = "—"
		a.quotaLevel.Retarget(0, now, .4)
	}
	if previous != a.quotaText {
		a.previousQuotaText = previous
		a.quotaMix = Tween{From: 0, To: 1, Start: now, Duration: .3}
	}
	qx, qfont, qtop := PanelWidth*.5-43, 31.0, 28.0
	if len(snapshot.Rows) > 0 {
		qx, qfont, qtop = 22, 29, 24
		if len(a.quotaText) >= 4 {
			qfont = 27
		}
	}
	a.quotaX.Retarget(qx, now, .69)
	a.quotaFont.Retarget(qfont, now, .77)
	a.quotaTop.Retarget(qtop, now, .56)
}

func (a *Animator) Frame(now float64) Scene {
	dt := math.Max(0, math.Min(.25, now-a.last))
	a.last = now
	if a.reducedMotion {
		dt = 0
	}
	a.waterPhase += dt * .22
	a.flowTime += dt
	a.rows = a.rows[:0]
	a.bands = a.bands[:0]
	height := a.height.Value(now)
	stage := a.appearance.frame(now)
	transition := now < a.height.Start+a.height.Duration || now < a.quotaX.Start+a.quotaX.Duration || now < a.quotaMix.Start+a.quotaMix.Duration || now < a.quotaLevel.Start+a.quotaLevel.Duration
	kept := 0
	for _, id := range a.order {
		p := a.poses[id]
		opacity := p.Opacity.Value(now)
		if p.gone && opacity < .001 {
			delete(a.poses, id)
			continue
		}
		a.order[kept] = id
		kept++
		flow := p.Flow.Value(now)
		p.phase += dt * flow
		rate := p.Rate.Value(now)
		step := int64(math.Round(rate * 10))
		if step != p.rateStep {
			p.rateLabel = strconv.FormatFloat(float64(step)/10, 'f', 1, 64) + "/s"
			p.rateStep = step
		}
		r := DrawRow{Row: p.Row, X: p.X.Value(now), Y: p.Y.Value(now), Font: p.Font.Value(now), Opacity: opacity,
			RateOpacity: p.RateOpacity.Value(now), Speed: p.rateLabel, Phase: p.phase, Flow: flow}
		r.PreviousModel, r.ModelMix = p.previousModel, p.modelMix.Value(now)
		r.WaveY = p.WaveY.Value(now)
		a.rows = append(a.rows, r)
		if now < p.Opacity.Start+p.Opacity.Duration || now < p.Y.Start+p.Y.Duration || now < p.RateOpacity.Start+p.RateOpacity.Duration || now < p.modelMix.Start+p.modelMix.Duration {
			transition = true
		}
	}
	a.order = a.order[:kept]
	// A small continuous phase field lets nearby regions move at different rates,
	// without drawing a boundary between conversations.
	var field [16]float64
	for k := range field {
		y := height * float64(k) / 15
		sum, weight := 0.0, 0.0
		for _, r := range a.rows {
			d := (y - r.WaveY) / math.Max(35, height*.28)
			w := r.Opacity / (1 + d*d)
			sum += math.Sin(r.Phase) * w
			weight += w
		}
		if weight > 0 {
			field[k] = sum / weight
		}
	}
	local := func(y float64) float64 {
		p := math.Max(0, math.Min(15, y/height*15))
		i := min(14, int(p))
		f := p - float64(i)
		return field[i]*(1-f) + field[i+1]*f
	}
	// Independent carrier waves and travelling envelopes deform the shape
	// locally. This is not translating or scaling a pre-drawn path.
	makeBand := func(k int, center, amplitude, width, phase, opacity float64) Band {
		b := Band{Count: 66, Tone: uint8(k % 5), Opacity: opacity, Phase: phase, Depth: float64(k%3) / 2}
		for j := 0; j <= 32; j++ {
			u := float64(j) / 32
			envelope := math.Sin(math.Pi * u)
			drift := local(center) * .24
			warp := .19 * math.Sin(u*4.1+phase*.39+float64(k))
			carrier := math.Sin(u*(5.4+.43*float64(k%3)) - phase*.67 + warp + drift)
			counter := .32 * math.Sin(u*10.3+phase*.41+float64(k)*1.7)
			slow := .20 * math.Sin(u*3.7-phase*.23+float64(k)*.8)
			packet := .75 + .25*math.Sin(u*6.7-phase*.52+float64(k))
			y := center + amplitude*(carrier+counter+slow)*packet*(.65+.35*envelope)
			y += (u - .5) * height * .10 * math.Cos(float64(k)*1.1+phase*.09)
			w := width * (.48 + .52*envelope*envelope) * (1 + .22*math.Sin(u*8.5+phase*.46+float64(k)))
			b.Points[j] = Point{u * PanelWidth, y - w}
			b.Points[65-j] = Point{u * PanelWidth, y + w}
			// A travelling caustic and finer sheen ride on the same geometry.
			b.Light[j] = .18 + .74*heroPulse(u, phase, k) + .11*math.Pow(.5+.5*math.Sin(u*16-phase*1.3+float64(k)), 4)
			b.Visibility[j] = stage.mask(u, k)
		}
		return b
	}
	for k := 0; k < 3; k++ {
		center := height * (.38 + .14*float64(k))
		op := .84 - .10*float64(k)
		if len(a.rows) == 0 {
			op *= .82
		}
		b := makeBand(k, center, 8+height*(.088-.012*float64(k)), .58-.09*float64(k), a.flowTime*.76+float64(k)*2.3, op)
		a.bands = append(a.bands, b)
	}
	for i, r := range a.rows {
		b := makeBand(3+i, r.WaveY, 6+height*.038, .22*math.Pow(r.Font/17.8, 1.2), r.Phase+float64(i)*1.3, r.Opacity*.52)
		a.bands = append(a.bands, b)
	}
	scene := Scene{Stage: stage, FlowTime: a.flowTime, TotalRows: a.totalRows, VisibleRows: a.layout.Visible, ViewportText: a.viewportText, Synthetic: a.synthetic, Width: PanelWidth, Height: height, QuotaY: height*.5 - a.quotaTop.Value(now), QuotaX: a.quotaX.Value(now), QuotaFont: a.quotaFont.Value(now), QuotaMix: a.quotaMix.Value(now), PreviousQuotaText: a.previousQuotaText, Rows: a.rows, Bands: a.bands, Quota: a.quota, QuotaText: a.quotaText, Transitioning: transition || stage.Transitioning}
	scene.PendingCompleted = a.pendingCompleted
	if a.quota.Known {
		level := a.quotaLevel.Value(now)
		amplitude := (4.0 + height*.024) * math.Sin(math.Pi*level)
		for j := 0; j <= 32; j++ {
			x := float64(j) / 32
			scene.Water[j] = Point{x * PanelWidth, height*(1-level) + (math.Sin(x*math.Pi*2.1-a.flowTime*.39)+.33*math.Sin(x*math.Pi*5+a.flowTime*.28))*amplitude}
		}
		scene.Water[33] = Point{PanelWidth, height}
		scene.Water[34] = Point{0, height}
		scene.WaterCount = 35
	}
	return scene
}

// SetReducedMotion freezes decorative phase without freezing the incoming data.
func (a *Animator) SetReducedMotion(reduced bool) { a.reducedMotion = reduced }
