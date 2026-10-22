"""Render/test the delivered standalone WASM UI; no AI imagery or model calls.
The video is an offline, fixed-timestep inspection recording, NOT an FPS test.
"""
from pathlib import Path
import base64, io, json, subprocess, hashlib, sys, shutil, os
from PIL import Image, ImageDraw, ImageFont
import numpy as np
from playwright.sync_api import sync_playwright

ROOT = Path(sys.argv[1]).resolve() if len(sys.argv)>1 else Path(__file__).resolve().parents[2]
OUT = ROOT / 'verification/native-optical'
OUT.mkdir(parents=True,exist_ok=True)
report = {'scope':'Shared Go compositor in Chromium/WASM, browser font masks; not Windows GDI/presentation', 'synthetic':True,'model_requests':0,'checks':{}}

def read_canvas(page):
    uri=page.evaluate("document.getElementById('panel').toDataURL('image/png')")
    return Image.open(io.BytesIO(base64.b64decode(uri.split(',',1)[1]))).convert('RGBA')

def draw(page,t,q=0,n=3,scale=2,quota=-1):
    page.evaluate('([t,n,q,s,k])=>{elapsed=t;nacreFrame(t,n,q,s,k);}',[t,n,q,scale,quota])
    return dict(page.evaluate('window.nacreTestState'))

def save(page,name):
    im=read_canvas(page);im.save(OUT/(name+'.png'));return im

def check(name,condition,detail=None):
    report['checks'][name]={'pass':bool(condition),'detail':detail}
    if not condition: raise AssertionError(f'{name}: {detail}')

with sync_playwright() as pw:
    browser=pw.chromium.launch(executable_path=(os.environ.get('CHROMIUM') or shutil.which('chromium') or shutil.which('chrome')),headless=True,args=['--no-sandbox','--disable-dev-shm-usage'])
    page=browser.new_page(viewport={'width':1180,'height':1060},device_scale_factor=1)
    errors=[];requests=[]
    page.on('pageerror',lambda e:errors.append(str(e)))
    page.on('request',lambda r:requests.append(r.url))
    # This environment disallows file:// navigation. Exact delivered file bytes
    # are loaded unchanged via set_content; no HTTP server or dependency CDN.
    page.set_content((ROOT/'verification/native-liveproof.html').read_text(),wait_until='load')
    page.wait_for_function('window.nacreFrame !== undefined')
    page.evaluate("()=>{paused=true;elapsed=0;nacreControl('reset',0);}")
    st=draw(page,0); im=save(page,'00-transparent-start')
    check('startup_zero_alpha',np.max(np.array(im)[:,:,3])==0)
    for i in range(1,241):
        t=i/30
        st=draw(page,t)
        if i in (15,30,45): save(page,f'01-entry-{i}')
    lit=save(page,'02-running-lit')
    report['running_stats']=st
    check('bounded_full_sprites',st['particles']<=64,st)
    check('two_scan_rings',st['rings']==2)
    check('real_radiance_above_unit',st['peak']>1,st['peak'])
    page.evaluate("nacreControl('light-off',8)")
    draw(page,8)
    unlit=save(page,'03-running-light-disabled')
    delta=np.abs(np.array(lit).astype('int16')[:,:,:3]-np.array(unlit).astype('int16')[:,:,:3]).max(axis=2)
    check('same_frame_glow_changes_pixels',np.count_nonzero(delta>4)>4000,{'pixels_over_4':int(np.count_nonzero(delta>4)),'max_delta':int(delta.max())})
    page.evaluate("nacreControl('light-on',8)")
    draw(page,8); a=read_canvas(page)
    draw(page,8); b=read_canvas(page)
    check('same_time_is_deterministic',a.tobytes()==b.tobytes())
    for i in range(1,17): draw(page,8+i/30)
    mov=read_canvas(page)
    change=np.abs(np.array(a).astype('int16')-np.array(mov).astype('int16')).max(axis=2)
    check('motion_has_actual_pixel_change',np.count_nonzero(change>5)>1000,int(np.count_nonzero(change>5)))
    page.evaluate("nacreControl('exit',8.533333333333333)")
    for i in range(1,58):
        st=draw(page,8.533333333333333+i/30)
        if i in (12,24,37): save(page,f'04-exit-{i}')
    gone=save(page,'05-transparent-end')
    check('exit_zero_alpha',np.max(np.array(gone)[:,:,3])==0)
    check('exit_clears_light_energy',st['active']==0,st)
    page.evaluate("nacreControl('reveal',10.433333333333334)")
    for i in range(1,60): draw(page,10.433333333333334+i/30)
    check('reopen_after_exit',np.count_nonzero(np.array(read_canvas(page))[:,:,3])>20000)
    draw(page,12.5,scale=1)
    native_size=save(page,'06-original-size')
    check('native_logical_width',native_size.width==400,native_size.size)
    st=draw(page,12.5,q=2)
    save(page,'07-economy')
    check('bounded_economy_sprites',st['particles']<=20,st)
    check('economy_retains_visible_emitters',st['peak']>1 and st['active']>0,st)
    for i in range(60): st=draw(page,13+i/30,q=0,n=16)
    save(page,'08-crowded')
    check('crowded_budget',st['particles']<=64 and st['rings']<=2,st)
    for i in range(60): draw(page,15+i/30,n=3,quota=-2)
    save(page,'09-unknown-quota')
    for i in range(30): draw(page,17+i/30,n=3,quota=-3)
    save(page,'10-stale-quota')
    for i in range(30): draw(page,18+i/30,n=3,quota=-1)
    draw(page,19,q=3)
    paused_a=read_canvas(page)
    draw(page,19.1,q=3)
    paused_b=read_canvas(page)
    # Exact rates may change at the next data update; under .2s no new data.
    diff=np.abs(np.array(paused_a).astype('int16')-np.array(paused_b).astype('int16')).max(axis=2)
    # Rate tweens intentionally remain live; compare decoration-only top/bottom.
    decorative=np.concatenate([diff[:48,:].ravel(),diff[-38:,:].ravel()])
    check('reduced_motion_freezes_decorative_regions',np.max(decorative)==0,{'decorative_max_delta':int(decorative.max()),'whole_frame_changed':int(np.count_nonzero(diff))})
    # Exercise actual toolbar and inspect layout at narrow viewport.
    page.set_viewport_size({'width':420,'height':980})
    page.evaluate("()=>{$('preset').value='3';$('quality').value='0';$('zoom').value='2';elapsed=20;draw();}")
    page.screenshot(path=str(OUT/'11-narrow-browser.png'))
    check('narrow_page_no_horizontal_overflow',page.evaluate('document.documentElement.scrollWidth <= innerWidth'))
    page.set_viewport_size({'width':1180,'height':1060})
    page.evaluate("()=>{$('stage').classList.add('dark');draw();}")
    page.screenshot(path=str(OUT/'12-dark-desktop-browser.png'))
    page.evaluate("$('stage').classList.remove('dark')")
    page.screenshot(path=str(OUT/'13-light-desktop-browser.png'))
    check('browser_errors',not errors,errors)
    check('no_external_requests',not requests,requests)
    report['all_passed']=all(x['pass'] for x in report['checks'].values())
    (OUT/'browser-verification.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    # Offline uniform-time capture; fixed 30 FPS encode does not imply a device
    # sustained 30 FPS. PNGs are direct compositor pixels at 2x inspection DPI.
    page.evaluate("()=>{elapsed=0;nacreControl('reset',0);nacreControl('light-on',0);}")
    font_path=next((p for p in [Path('/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'),Path(os.environ.get('WINDIR','C:/Windows'))/'Fonts/segoeui.ttf'] if p.exists()),None)
    font=ImageFont.truetype(str(font_path),19) if font_path else ImageFont.load_default(size=19)
    small=ImageFont.truetype(str(font_path),14) if font_path else ImageFont.load_default(size=14)
    output=ROOT/'Nacre_0.4.1_实代码动效.mp4'
    encoder=subprocess.Popen(['ffmpeg','-hide_banner','-loglevel','error','-y','-f','rawvideo','-pix_fmt','rgb24','-s','960x580','-r','30','-i','-','-an','-c:v','libx264','-threads','2','-preset','veryfast','-crf','18','-pix_fmt','yuv420p','-movflags','+faststart',str(output)],stdin=subprocess.PIPE)
    try:
        for first in range(0,360,12):
            frames=page.evaluate("""first=>{const frames=[];for(let i=first;i<first+12;i++){const t=i/30;elapsed=t;if(i===255)nacreControl('exit',8.5);nacreFrame(t,3,0,2,-1);frames.push(document.getElementById('panel').toDataURL('image/png'));}return frames;}""",first)
            for j,png in enumerate(frames):
                t=(first+j)/30
                panel=Image.open(io.BytesIO(base64.b64decode(png.split(',',1)[1]))).convert('RGBA')
                frame=Image.new('RGB',(960,580),(224,226,224))
                frame.paste(panel,((960-panel.width)//2,116),panel)
                d=ImageDraw.Draw(frame)
                d.text((40,28),'NACRE 0.4.1  /  OPTICAL COMPOSITOR',font=font,fill=(61,66,64))
                d.text((40,59),'Actual code rendering | Synthetic data | 2x inspection | Browser glyph masks',font=small,fill=(91,96,91))
                state='ASSEMBLY' if t<1.72 else ('DISSOLVE' if t>=8.5 and t<10.02 else ('HIDDEN' if t>=10.02 else 'RUNNING'))
                d.text((40,528),f'{state}   {t:05.2f}s',font=font,fill=(61,66,64))
                d.text((40,557),'Fixed-time offline capture. Not a Windows screen recording or a frame-rate benchmark.',font=small,fill=(91,96,91))
                encoder.stdin.write(frame.tobytes())
        encoder.stdin.close();rc=encoder.wait(timeout=30)
        check('mp4_encoded',rc==0,{'frames':360,'fps':30,'seconds':12,'bytes':output.stat().st_size})
    except Exception:
        encoder.kill();raise
    check('browser_errors',not errors,errors)
    check('no_external_requests',not requests,requests)
    report['browser_version']=browser.version
    report['html_load_method']='Exact file content via Playwright set_content because sandbox blocks file:// navigation; double-click route not tested here'
    browser.close()
report['all_passed']=all(x['pass'] for x in report['checks'].values())
(OUT/'browser-verification.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
print(json.dumps(report,ensure_ascii=False,indent=2))
