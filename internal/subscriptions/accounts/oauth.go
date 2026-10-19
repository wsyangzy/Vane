package accounts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// JWTClaims is only for metadata/expiry discovery. It makes no authentication
// assertion; the fixed official endpoint validates the supplied access token.
func JWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	data, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || len(data) > 64*1024 {
		return nil
	}
	var v map[string]any
	_ = json.Unmarshal(data, &v)
	return v
}
func Claim(v map[string]any, key string) string { s, _ := v[key].(string); return s }
func TokenExpiry(token string) time.Time {
	v, ok := JWTClaims(token)["exp"].(float64)
	if !ok || v <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(v), 0)
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func Exchange(ctx context.Context, client *http.Client, endpoint string, payload map[string]any) (TokenResponse, error) {
	var result TokenResponse
	body, _ := json.Marshal(payload)
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result, errors.New("subscription_refresh_request_failed")
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return result, errors.New("subscription_refresh_transport_failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	defer clear(data)
	if err != nil || len(data) > 1<<20 {
		return result, errors.New("subscription_refresh_response_failed")
	}
	if resp.StatusCode != 200 {
		var p struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(data, &p)
		var code string
		if json.Unmarshal(p.Error, &code) != nil {
			var o struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(p.Error, &o)
			code = o.Code
		}
		return result, SafeRefreshError(resp.StatusCode, code)
	}
	if json.Unmarshal(data, &result) != nil || !ValidToken(result.AccessToken) || (result.RefreshToken != "" && !ValidToken(result.RefreshToken)) || result.ExpiresIn < 0 || result.ExpiresIn > 31536000 {
		return TokenResponse{}, errors.New("subscription_refresh_response_invalid")
	}
	return result, nil
}
