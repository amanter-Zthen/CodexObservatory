// Package capturehome owns only a marked configuration block in Codex Home.
package capturehome

import (
	"bytes"
	"fmt"
	"strings"
)

const Begin = "# BEGIN CodexObservatory native capture v1"
const End = "# END CodexObservatory native capture v1"

// HasBlock reports whether the marked block is present. It never mutates input.
func HasBlock(input []byte) bool {
	return bytes.Count(input, []byte(Begin)) == 1 && bytes.Count(input, []byte(End)) == 1
}

// BlockPort returns the port of the HTTPS_PROXY entry inside the marked block,
// or 0 when the block is absent, malformed, or has no usable port. It is the
// shared read-only counterpart of Install so no second parser is written.
func BlockPort(input []byte) int {
	if !HasBlock(input) {
		return 0
	}
	start := bytes.Index(input, []byte(Begin))
	finish := bytes.Index(input, []byte(End))
	if start < 0 || finish < start {
		return 0
	}
	block := string(input[start : finish+len(End)])
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "HTTPS_PROXY") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "'\"")
		i := strings.LastIndex(value, ":")
		if i < 0 {
			return 0
		}
		n := 0
		for _, r := range value[i+1:] {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int(r-'0')
		}
		return n
	}
	return 0
}

func Remove(input []byte) ([]byte, error) {
	start := bytes.Index(input, []byte(Begin))
	finish := bytes.Index(input, []byte(End))
	if start < 0 && finish < 0 {
		return append([]byte{}, input...), nil
	}
	if start < 0 || finish < start || bytes.Count(input, []byte(Begin)) != 1 || bytes.Count(input, []byte(End)) != 1 {
		return nil, fmt.Errorf("capture configuration block requires review")
	}
	if start > 0 && input[start-1] != '\n' {
		return nil, fmt.Errorf("capture configuration marker requires its own line")
	}
	finish += len(End)
	if finish < len(input) && input[finish] == '\r' {
		finish++
	}
	if finish < len(input) && input[finish] == '\n' {
		finish++
	} else if finish < len(input) {
		return nil, fmt.Errorf("capture configuration marker requires its own line")
	}
	return append(append([]byte{}, input[:start]...), input[finish:]...), nil
}

func Install(input []byte, proxyURL, certificate string) ([]byte, error) {
	base, err := Remove(input)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(base), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		line = strings.TrimPrefix(line, "export ")
		key, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "CODEX_CA_CERTIFICATE":
			return nil, fmt.Errorf("existing native network configuration requires review")
		}
	}
	for _, value := range []string{proxyURL, certificate} {
		if strings.ContainsAny(value, "'\r\n") {
			return nil, fmt.Errorf("native capture configuration value is invalid")
		}
	}
	if len(base) > 0 && base[len(base)-1] != '\n' {
		base = append(base, '\n')
	}
	block := Begin + "\nHTTPS_PROXY='" + proxyURL + "'\nHTTP_PROXY='" + proxyURL + "'\nSSL_CERT_FILE='" + certificate + "'\nNO_PROXY=''\n" + End + "\n"
	return append(base, []byte(block)...), nil
}
