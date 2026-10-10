package hub

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Each host seals mail to the receiving host's X25519 key and signs it with
// its own Ed25519 key, so the hub carries mail it cannot read or forge
// (design §19.4). Go's standard library only.

// Keys are this host's key pairs, in ~/.sunstack/keys.json (0600).
type Keys struct {
	Seal []byte `json:"seal"` // X25519 private key
	Sign []byte `json:"sign"` // Ed25519 private key (seed and public key)
}

// PublicKeys are a host's public halves, as the roster carries them.
type PublicKeys struct {
	Seal string `json:"seal"` // base64 X25519 public key
	Sign string `json:"sign"` // base64 Ed25519 public key
}

func keysPath() string { return filepath.Join(core.Home(), "keys.json") }

// LoadKeys reads this host's keys, making them on first use.
func LoadKeys() (*Keys, error) {
	var k Keys
	if err := readJSON(keysPath(), &k); err == nil && len(k.Seal) == 32 && len(k.Sign) == ed25519.PrivateKeySize {
		return &k, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	sk, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	_, sg, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	k = Keys{Seal: sk.Bytes(), Sign: sg}
	if err := writeJSON(keysPath(), &k, 0o600); err != nil {
		return nil, err
	}
	return &k, nil
}

// Public returns the public halves.
func (k *Keys) Public() PublicKeys {
	sk, _ := ecdh.X25519().NewPrivateKey(k.Seal)
	return PublicKeys{
		Seal: base64.StdEncoding.EncodeToString(sk.PublicKey().Bytes()),
		Sign: base64.StdEncoding.EncodeToString(ed25519.PrivateKey(k.Sign).Public().(ed25519.PublicKey)),
	}
}

// Fingerprint is a short, readable digest of a host's public keys, for
// comparing on two screens.
func (p PublicKeys) Fingerprint() string {
	sum := sha256.Sum256([]byte(p.Seal + "\n" + p.Sign))
	s := strings.ToUpper(base64.RawStdEncoding.EncodeToString(sum[:12]))
	s = strings.NewReplacer("+", "X", "/", "Y").Replace(s)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func (p PublicKeys) valid() bool {
	a, err1 := base64.StdEncoding.DecodeString(p.Seal)
	b, err2 := base64.StdEncoding.DecodeString(p.Sign)
	return err1 == nil && err2 == nil && len(a) == 32 && len(b) == ed25519.PublicKeySize
}

// Letter is what only the receiving host reads.
type Letter struct {
	From        string `json:"from"` // the sender on its host: user, or <team>/<agent>
	To          string `json:"to"`   // the address on the receiving host
	Type        string `json:"type"`
	ReplyTo     string `json:"reply_to,omitempty"`
	FromSession string `json:"from_session,omitempty"`
	At          string `json:"at"`
	Body        string `json:"body"`
}

func mailAAD(id, from, to string) []byte { return []byte(id + "|" + from + "|" + to) }

// seal encrypts a letter to the receiving host and signs the result.
func seal(k *Keys, to PublicKeys, m *Mail, l *Letter) error {
	rpub, err := base64.StdEncoding.DecodeString(to.Seal)
	if err != nil {
		return err
	}
	peer, err := ecdh.X25519().NewPublicKey(rpub)
	if err != nil {
		return err
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	shared, err := eph.ECDH(peer)
	if err != nil {
		return err
	}
	aad := mailAAD(m.ID, m.FromHost, m.ToHost)
	key, err := hkdf.Key(sha256.New, shared, append(eph.PublicKey().Bytes(), rpub...), "sunstack mail v1", 32)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(l)
	if err != nil {
		return err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, plain, aad)
	m.Eph = base64.StdEncoding.EncodeToString(eph.PublicKey().Bytes())
	m.Sealed = base64.StdEncoding.EncodeToString(ct)
	m.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(k.Sign), signed(m)))
	return nil
}

func signed(m *Mail) []byte {
	return []byte(m.ID + "|" + m.FromHost + "|" + m.ToHost + "|" + m.Eph + "|" + m.Sealed)
}

// open checks the sender's signature against its pinned key and decrypts.
func open(k *Keys, from PublicKeys, m *Mail) (*Letter, error) {
	spub, err := base64.StdEncoding.DecodeString(from.Sign)
	if err != nil || len(spub) != ed25519.PublicKeySize {
		return nil, errors.New("no key pinned for the sending host")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Sig)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(spub), signed(m), sig) {
		return nil, errors.New("bad signature: the message was changed, or not sent by that host")
	}
	epub, err := base64.StdEncoding.DecodeString(m.Eph)
	if err != nil {
		return nil, errors.New("bad envelope")
	}
	eph, err := ecdh.X25519().NewPublicKey(epub)
	if err != nil {
		return nil, errors.New("bad envelope")
	}
	sk, err := ecdh.X25519().NewPrivateKey(k.Seal)
	if err != nil {
		return nil, err
	}
	shared, err := sk.ECDH(eph)
	if err != nil {
		return nil, errors.New("bad envelope")
	}
	key, err := hkdf.Key(sha256.New, shared, append(epub, sk.PublicKey().Bytes()...), "sunstack mail v1", 32)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(m.Sealed)
	if err != nil {
		return nil, errors.New("bad envelope")
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(ct) < gcm.NonceSize() {
		return nil, errors.New("bad envelope")
	}
	plain, err := gcm.Open(nil, ct[:gcm.NonceSize()], ct[gcm.NonceSize():], mailAAD(m.ID, m.FromHost, m.ToHost))
	if err != nil {
		return nil, errors.New("the message cannot be opened with this host's key")
	}
	var l Letter
	if err := json.Unmarshal(plain, &l); err != nil {
		return nil, errors.New("bad letter")
	}
	return &l, nil
}

// Pin is the key this host trusts for another host.
type Pin struct {
	Name    string      `json:"name"`
	Keys    PublicKeys  `json:"keys"`
	Changed *PublicKeys `json:"changed,omitempty"` // a different key the roster now shows
}

func pinsPath() string { return filepath.Join(remoteDir(), "pins.json") }

func loadPins() map[string]*Pin {
	pins := map[string]*Pin{}
	_ = readJSON(pinsPath(), &pins)
	return pins
}

// pinRoster pins hosts seen for the first time and marks changed keys.
// A host seen for the first time under a name another pinned host had is
// pinned as changed, so mail waits for the user's trust: the hub cannot
// hand a known name to a new key.
func pinRoster(r *Roster) error {
	pins := loadPins()
	named := map[string]string{} // lower-case name -> ID, before this roster
	for id, p := range pins {
		named[strings.ToLower(p.Name)] = id
	}
	changed := false
	for _, h := range r.Hosts {
		if !h.Keys.valid() || !idRe.MatchString(h.ID) {
			continue
		}
		p := pins[h.ID]
		switch {
		case p == nil:
			if id, ok := named[strings.ToLower(h.Name)]; ok && id != h.ID {
				k := h.Keys
				pins[h.ID] = &Pin{Name: h.Name, Changed: &k}
			} else {
				pins[h.ID] = &Pin{Name: h.Name, Keys: h.Keys}
			}
			changed = true
		case p.Keys != h.Keys && (p.Changed == nil || *p.Changed != h.Keys):
			k := h.Keys
			p.Changed, changed = &k, true
		case p.Keys == h.Keys && p.Changed != nil:
			p.Changed, changed = nil, true
		}
		if p != nil && p.Name != h.Name {
			p.Name, changed = h.Name, true
		}
	}
	if !changed {
		return nil
	}
	return writeJSON(pinsPath(), pins, 0o600)
}

// Trust accepts a host's new key after the user compared fingerprints.
func Trust(name string) (string, error) {
	pins := loadPins()
	var hit, same *Pin
	for _, p := range pins {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		if p.Changed == nil {
			same = p
			continue
		}
		if hit != nil {
			return "", failErr("ambiguous", "two hosts called %s have new keys; ask the hub's user to revoke the wrong one", name)
		}
		hit = p
	}
	switch {
	case hit != nil:
		hit.Keys, hit.Changed = *hit.Changed, nil
		return hit.Keys.Fingerprint(), writeJSON(pinsPath(), pins, 0o600)
	case same != nil:
		return "", failErr("unchanged", "%s's key has not changed (%s)", same.Name, same.Keys.Fingerprint())
	}
	return "", failErr("not_found", "no pinned host %s (sunstack org keys lists them)", name)
}

// KeysText is `sunstack org keys`: this host's fingerprint and the ones it
// trusts, to compare on two screens.
func KeysText() (string, error) {
	k, err := LoadKeys()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("This host (" + core.ThisHost().Name + "): " + k.Public().Fingerprint() + "\n")
	pins := loadPins()
	if len(pins) > 0 {
		b.WriteString("Trusted keys of the other hosts:\n")
	}
	for id, p := range pins {
		if id == core.ThisHost().ID {
			continue
		}
		line := "  " + p.Name + ": " + p.Keys.Fingerprint()
		if p.Changed != nil {
			line += "  KEY CHANGED to " + p.Changed.Fingerprint() + " (mail refused; compare, then sunstack org trust " + p.Name + ")"
		}
		b.WriteString(line + "\n")
	}
	return b.String(), nil
}
