package bilibili

import (
	"context"
	"crypto/md5" // Bilibili's WBI protocol requires MD5, not a security primitive here.
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/discovery"
)

// envelope is the common Bilibili API response wrapper.
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) get(ctx context.Context, endpoint string, params url.Values, target any, allowAnonymousNav bool) error {
	return c.getFrom(ctx, c.baseURL, "https://space.bilibili.com/", endpoint, params, target, allowAnonymousNav)
}

// getFrom performs a credential-scoped API request and maps provider protocol
// failures into stable platform errors. Feature files only see decoded data.
func (c *Client) getFrom(ctx context.Context, origin, referer, endpoint string, params url.Values, target any, allowAnonymousNav bool) error {
	body, err := c.fetchEnvelope(ctx, origin, referer, endpoint, params)
	if err != nil {
		return err
	}
	if body.Code != 0 && !(allowAnonymousNav && body.Code == -101) {
		switch body.Code {
		case -352, -412, -799, -509:
			return c.restrictedAPI(body.Code)
		case -101:
			return &discovery.PlatformError{Message: "此内容需要 B站登录态，或现有登录态已过期。"}
		case -404, 62002:
			return &discovery.PlatformError{Message: "没有找到这位 UP 主或视频，内容也可能已经删除。"}
		default:
			return &discovery.PlatformError{Message: "B站未能提供这份内容，请确认地址后重试。"}
		}
	}
	if len(body.Data) == 0 || string(body.Data) == "null" {
		return &discovery.PlatformError{Message: "B站返回了不完整的数据，请稍后重试。"}
	}
	if err := json.Unmarshal(body.Data, target); err != nil {
		return &discovery.PlatformError{Message: "B站数据格式发生变化，请稍后重试。"}
	}
	return nil
}

// fetchEnvelope retains the platform code/message for callers that need to
// explain a per-part failure. It deliberately never exposes request headers.
func (c *Client) fetchEnvelope(ctx context.Context, origin, referer, endpoint string, params url.Values) (envelope, error) {
	if c.refresher != nil {
		_ = c.refresher.Ensure(ctx)
	}
	c.mu.Lock()
	blocked := time.Now().Before(c.restrictedUntil)
	c.mu.Unlock()
	if blocked {
		return envelope{}, &discovery.PlatformError{Message: "B站暂时限制了请求。请稍后再试，或直接打开原站浏览。", Restricted: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return envelope{}, err
	}
	requestUserAgent := userAgent
	if c.userAgentProvider != nil {
		if value := c.userAgentProvider(req.URL.String()); value != "" {
			requestUserAgent = value
		}
	}
	req.Header.Set("User-Agent", requestUserAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "application/json")
	cookie := c.cookie
	if c.cookieProvider != nil {
		cookie = c.cookieProvider(req.URL.String())
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return envelope{}, ctx.Err()
		}
		return envelope{}, &discovery.PlatformError{Message: "暂时无法连接 B站，请检查网络后重试。"}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden {
		return envelope{}, c.restrictedHTTP(response.StatusCode, safeSubtitleText(response.Header.Get("Content-Type"), 80))
	}
	if response.StatusCode != http.StatusOK {
		return envelope{}, &discovery.PlatformError{Message: "B站服务暂时不可用，请稍后重试。"}
	}
	var body envelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&body); err != nil {
		return envelope{}, &discovery.PlatformError{Message: "B站返回了无法识别的数据，请稍后重试。"}
	}
	return body, nil
}

func (c *Client) restricted() error {
	return c.restrictedFor("", "")
}

func (c *Client) restrictedAPI(code int) error {
	return c.restrictedFor(strconv.Itoa(code), "接口状态 "+strconv.Itoa(code))
}

func (c *Client) restrictedHTTP(status int, contentType string) error {
	cause := "HTTP " + strconv.Itoa(status)
	if contentType != "" {
		cause += " " + contentType
	}
	return c.restrictedFor(strconv.Itoa(status), cause)
}

func (c *Client) restrictedFor(code, cause string) error {
	c.mu.Lock()
	c.restrictedUntil = time.Now().Add(time.Minute)
	c.mu.Unlock()
	message := "B站暂时限制了请求。请等待一分钟后重试；登录态或网络环境也可能影响访问。"
	if cause != "" {
		message = "B站暂时限制了请求（" + cause + "），请等待一分钟后重试；登录态或网络环境也可能影响访问。"
	}
	return &discovery.PlatformError{Message: message, Code: code, Restricted: true}
}

func (c *Client) mixinKey(ctx context.Context) (string, error) {
	c.mu.Lock()
	key, expires := c.key, c.keyExpires
	c.mu.Unlock()
	if key != "" && time.Now().Before(expires) {
		return key, nil
	}
	var data struct {
		WBI struct {
			Image string `json:"img_url"`
			Sub   string `json:"sub_url"`
		} `json:"wbi_img"`
	}
	if err := c.get(ctx, "/x/web-interface/nav", nil, &data, true); err != nil {
		return "", err
	}
	key, err := wbiMixinKey(data.WBI.Image, data.WBI.Sub)
	if err != nil {
		return "", &discovery.PlatformError{Message: "暂时无法获取 B站接口签名，请稍后重试。"}
	}
	c.mu.Lock()
	c.key, c.keyExpires = key, time.Now().Add(10*time.Minute)
	c.mu.Unlock()
	return key, nil
}

func wbiMixinKey(imageURL, subURL string) (string, error) {
	image := strings.TrimSuffix(path.Base(imageURL), ".png")
	sub := strings.TrimSuffix(path.Base(subURL), ".png")
	return deriveKey(image + sub)
}

// The permutation is a protocol constant documented by bilibili-api-collect.
func deriveKey(raw string) (string, error) {
	if len(raw) != 64 {
		return "", fmt.Errorf("invalid WBI key length")
	}
	indices := [...]int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13}
	key := make([]byte, len(indices))
	for i, index := range indices {
		key[i] = raw[index]
	}
	return string(key), nil
}

func sign(params url.Values, key string, timestamp int64) {
	params.Set("wts", strconv.FormatInt(timestamp, 10))
	replacer := strings.NewReplacer("!", "", "'", "", "(", "", ")", "", "*", "")
	for name, values := range params {
		for i, value := range values {
			values[i] = replacer.Replace(value)
		}
		params[name] = values
	}
	query := strings.ReplaceAll(params.Encode(), "+", "%20")
	digest := md5.Sum([]byte(query + key))
	params.Set("w_rid", hex.EncodeToString(digest[:]))
}
