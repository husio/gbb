package surf

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

func NewCookieCache(prefix string, secret []byte) (UnboundCacheService, error) {
	block, err := aes.NewCipher(adjustKeySize(secret))
	if err != nil {
		return nil, fmt.Errorf("cannot create cipher block: %s", err)
	}
	cc := unboundCookieCache{
		prefix: prefix,
		secret: block,
	}
	return &cc, nil
}

// adjustKeySize trim given secret to biggest acceptable by AES implementation
// key block. If given secret is too short to be used as AES key, it is
// returned without modifications
func adjustKeySize(secret []byte) []byte {
	size := len(secret)
	if size > 32 {
		return secret[:32]
	}
	if size > 24 {
		return secret[:24]
	}
	if size > 16 {
		return secret[:16]
	}
	return secret
}

type unboundCookieCache struct {
	secret cipher.Block
	prefix string
}

func (c *unboundCookieCache) Bind(w http.ResponseWriter, r *http.Request) CacheService {
	return &cookieCache{
		prefix: c.prefix,
		secret: c.secret,
		w:      w,
		r:      r,
		staged: make(map[string][]byte),
	}
}

type cookieCache struct {
	w      http.ResponseWriter
	r      *http.Request
	prefix string
	secret cipher.Block

	staged map[string][]byte
}

func (s *cookieCache) Get(ctx context.Context, key string, dest interface{}) error {
	defer CurrentTrace(ctx).Begin("cookie cache get",
		"key", key,
	).Finish()

	if rawVal, ok := s.staged[key]; ok {
		if err := json.Unmarshal(rawVal, dest); err != nil {
			return fmt.Errorf("cannot decode staged value: %s", err)
		}
		return nil
	}

	c, err := s.r.Cookie(s.prefix + key)
	if err != nil {
		return ErrMiss
	}

	// if cookie cannot be decoded or signature is invalid, ErrMiss
	// is returned. User cannot deal with such issue, so no need to
	// bother with the details

	rawData, err := s.decrypt(c.Value)
	if err != nil {
		return ErrMiss
	}

	rawPayload := rawData[:len(rawData)-4]
	rawExp := rawData[len(rawData)-4:]
	exp := time.Unix(int64(binary.LittleEndian.Uint32(rawExp)), 0)
	if exp.Before(time.Now()) {
		s.del(key)
		return ErrMiss
	}

	if err := json.Unmarshal(rawPayload, dest); err != nil {
		return fmt.Errorf("cannot deserialize value: %s", err)
	}
	return nil
}

func (s *cookieCache) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	defer CurrentTrace(ctx).Begin("cookie cache set",
		"key", key,
		"exp", fmt.Sprint(exp),
	).Finish()

	return s.set(key, value, exp)
}

func (s *cookieCache) set(key string, value interface{}, exp time.Duration) error {
	rawPayload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot serialize value: %s", err)
	}

	expAt := time.Now().Add(exp)
	rawExp := make([]byte, 4)
	binary.LittleEndian.PutUint32(rawExp, uint32(expAt.Unix()))

	rawData := append(rawPayload, rawExp...)
	payload, err := s.encrypt(rawData)
	if err != nil {
		return err
	}

	http.SetCookie(s.w, &http.Cookie{
		Name:     s.prefix + key,
		Value:    payload,
		Path:     "/",
		Expires:  expAt,
		HttpOnly: true,
		//Secure:   true,
	})
	s.staged[key] = rawPayload
	return nil
}

func (s *cookieCache) encrypt(data []byte) (string, error) {
	cipherText := make([]byte, ivSize+len(data))

	iv := cipherText[:ivSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(s.secret, iv)
	stream.XORKeyStream(cipherText[aes.BlockSize:], data)

	return base64.URLEncoding.EncodeToString(cipherText), nil
}

const ivSize = aes.BlockSize

func (s *cookieCache) decrypt(payload string) ([]byte, error) {
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("malformed data")
	}
	if len(raw) < ivSize {
		return nil, errors.New("message too short")
	}

	data := raw[ivSize:]
	stream := cipher.NewCFBDecrypter(s.secret, raw[:ivSize])
	stream.XORKeyStream(data, data)
	return data, nil
}

func (s *cookieCache) SetNx(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	if _, ok := s.staged[key]; ok {
		return ErrConflict
	}
	if _, err := s.r.Cookie(s.prefix + key); err == nil {
		// TODO check if valid and not expired
		return ErrConflict
	}

	return s.set(key, value, exp)
}

func (s *cookieCache) Del(ctx context.Context, key string) error {
	defer CurrentTrace(ctx).Begin("cookie cache del",
		"key", key,
	).Finish()

	return s.del(key)
}

func (s *cookieCache) del(key string) error {
	existed := false
	if _, ok := s.staged[key]; ok {
		existed = true
		delete(s.staged, key)
	}

	// TODO: deleting does not remove it from the request
	if _, err := s.r.Cookie(s.prefix + key); err == nil {

		// TODO: check if cookie value is not expired

		http.SetCookie(s.w, &http.Cookie{
			Name:    s.prefix + key,
			Value:   "",
			Path:    "/",
			Expires: time.Time{},
			MaxAge:  -1,
		})
		existed = true
	}

	if !existed {
		return ErrMiss
	}
	return nil
}
