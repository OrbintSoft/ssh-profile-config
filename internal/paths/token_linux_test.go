//go:build linux

package paths

import (
	"encoding/hex"
	"testing"
)

func TestSocketToken(t *testing.T) {
	tok := SocketToken()
	if tok == "" {
		t.Skip("user keyring unavailable")
	}
	if len(tok) != tokenByteLen*2 {
		t.Errorf("token length = %d, want %d", len(tok), tokenByteLen*2)
	}
	if _, err := hex.DecodeString(tok); err != nil {
		t.Errorf("token is not hex: %v", err)
	}
	if again := SocketToken(); again != tok {
		t.Errorf("token not stable within a login: %q then %q", tok, again)
	}
}
