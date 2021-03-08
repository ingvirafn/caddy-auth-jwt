// Copyright 2020 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package backends

import (
	"crypto/rsa"
	"encoding/json"
	"io/ioutil"
	"net/http"

	jwtlib "github.com/dgrijalva/jwt-go"
	"github.com/greenpau/caddy-auth-jwt/pkg/errors"
	oauth2 "github.com/greenpau/caddy-auth-portal/pkg/backends/oauth2"
	"go.uber.org/zap"
)

var defaultKeyID = "0"

// TokenBackend is the interface to provide key material.
type TokenBackend interface {
	ProvideKey(token *jwtlib.Token) (interface{}, error)
}

// SecretKeyTokenBackend hold symentric keys from HS family.
type SecretKeyTokenBackend struct {
	secret []byte
}

// NewSecretKeyTokenBackend returns SecretKeyTokenBackend instance.
func NewSecretKeyTokenBackend(s string) (*SecretKeyTokenBackend, error) {
	if len(s) < 16 {
		return nil, errors.ErrInvalidSecretLength
	}
	b := &SecretKeyTokenBackend{
		secret: []byte(s),
	}
	return b, nil
}

// ProvideKey provides key material from SecretKeyTokenBackend.
func (b *SecretKeyTokenBackend) ProvideKey(token *jwtlib.Token) (interface{}, error) {
	if _, validMethod := token.Method.(*jwtlib.SigningMethodHMAC); !validMethod {
		return nil, errors.ErrUnexpectedSigningMethod.WithArgs("HS", token.Header["alg"])
	}
	return b.secret, nil
}

// RSAKeyTokenBackend hold asymentric keys from RS family.
type RSAKeyTokenBackend struct {
	secrets map[string]interface{}
}

// NewRSAKeyTokenBackend returns RSKeyTokenBackend instance.
func NewRSAKeyTokenBackend(k map[string]interface{}) *RSAKeyTokenBackend {
	b := &RSAKeyTokenBackend{
		secrets: k,
	}
	return b
}

// ProvideKey provides key material from RSKeyTokenBackend.
func (b *RSAKeyTokenBackend) ProvideKey(token *jwtlib.Token) (interface{}, error) {
	if _, validMethod := token.Method.(*jwtlib.SigningMethodRSA); !validMethod {
		return nil, errors.ErrUnexpectedSigningMethod.WithArgs("RS", token.Header["alg"])
	}

	// check if we have a "kid" in the header we can use...
	if kid, ok := token.Header["kid"].(string); ok {
		if val, ok := b.secrets[kid]; ok {
			switch key := val.(type) {
			case *rsa.PrivateKey:
				return &key.PublicKey, nil
			case *rsa.PublicKey:
				return key, nil
			}
			// it should never get here
			// becuase only RSA keys should
			// be put into the b.secrets field
		}
		return nil, errors.ErrUnexpectedKID
	}

	// no kid, then we should have a "0", as that's the default value
	if val, ok := b.secrets[defaultKeyID]; ok {
		switch key := val.(type) {
		case *rsa.PrivateKey:
			return &key.PublicKey, nil
		case *rsa.PublicKey:
			return key, nil
		}
	}

	return nil, errors.ErrNoRSAKeyFound
}

func FetchKeysURL(jwks_uri string) (map[string]interface{}, error) {

	secrets := make(map[string]interface{})

	resp, err := http.Get(jwks_uri)
	if err != nil {
		return nil, err
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	data := make(map[string]interface{})

	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, err
	}

	if _, exists := data["keys"]; !exists {
		return nil, errors.ErrBackendOauthJwksResponseKeysNotFound
	}

	jwksJSON, err := json.Marshal(data["keys"])
	if err != nil {
		return nil, errors.ErrBackendOauthJwksKeysParseFailed.WithArgs(err)
	}

	keys := []*oauth2.JwksKey{}
	if err := json.Unmarshal(jwksJSON, &keys); err != nil {
		return nil, err
	}

	if len(keys) < 1 {
		return nil, errors.ErrBackendOauthJwksKeysNotFound
	}

	for _, k := range keys {
		if err := k.Validate(); err != nil {
			return nil, errors.ErrBackendOauthJwksInvalidKey.WithArgs(err)
		}
		secrets[k.KeyID] = k.GetPublicKey()
	}

	return secrets, nil
}

type JwksUriBackend struct {
	jwks_uri   string
	publicKeys map[string]*rsa.PublicKey
	logger     *zap.Logger
}
