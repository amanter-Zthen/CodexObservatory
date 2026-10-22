package capturehome

import (
	"bytes"
	"strings"
	"testing"
)

func TestManagedBlockPreservesUserLinesAndRemovesOnlyOwnedSettings(t *testing.T) {
	base := []byte("\ufeffUSER_VALUE='value'\r\n# retained comment\r\n")
	first, err := Install(base, "http://observer:synthetic@127.0.0.1:23001", `D:\Local\native-capture\ca.pem`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(first, "http://observer:replacement@127.0.0.1:23002", `D:\Local\native-capture\ca.pem`)
	if err != nil || bytes.Count(second, []byte(Begin)) != 1 || bytes.Contains(second, []byte("synthetic@")) {
		t.Fatal("managed update failed", err)
	}
	second = append(second, []byte("OTHER_SETTING='new'\r\n")...)
	removed, err := Remove(second)
	if err != nil || !bytes.Equal(removed, append(base, []byte("OTHER_SETTING='new'\r\n")...)) {
		t.Fatal("unrelated settings changed", err)
	}
	if strings.Contains(string(first), "CODEX_CA_CERTIFICATE=") {
		t.Fatal("Codex-filtered key was used")
	}
}

func TestExistingNetworkSettingsAndMalformedMarkersNeedReview(t *testing.T) {
	for _, input := range []string{"HTTPS_PROXY='user-proxy'\n", "export SSL_CERT_FILE='user-ca'\n", Begin + "\n", End + "\n", "text" + Begin + "\n" + End + "\n"} {
		if _, err := Install([]byte(input), "http://observer:synthetic@127.0.0.1:23001", "ca.pem"); err == nil {
			t.Fatal("conflicting settings accepted")
		}
	}
}
