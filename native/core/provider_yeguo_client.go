package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const yeguoBaseURL = "https://delta.ygrwdsgt.cc"

var (
	errYeguoDecode    = errors.New("野果接口响应校验或解码失败")
	yeguoModuleImport = regexp.MustCompile(`(?s)import\s*\{([^{};]+)\}\s*from\s*["'\x60]([^"'\x60]+)["'\x60]`)
	yeguoPublicField  = regexp.MustCompile(`\b(version|mode|padding|key|iv|sign_key)\s*:\s*(?:[a-zA-Z_$][a-zA-Z0-9_$]*\(\s*)?["'\x60]([^"'\x60\\\r\n]{1,512})["'\x60]`)
)

type yeguoAccess struct {
	base       string
	key        []byte
	iv         []byte
	signKey    []byte
	identifier string
	loadedAt   time.Time
}

type yeguoAccessCall struct {
	done   chan struct{}
	access *yeguoAccess
	err    error
}

type yeguoAPIClient struct {
	downloader *Downloader
	site       string
	mu         sync.Mutex
	access     *yeguoAccess
	pending    *yeguoAccessCall
}

func (d *Downloader) yeguoClient() *yeguoAPIClient {
	d.yeguoOnce.Do(func() {
		d.yeguo = &yeguoAPIClient{downloader: d, site: d.providerBaseURL(sourceYeguo)}
	})
	return d.yeguo
}

func (client *yeguoAPIClient) configuration(ctx context.Context) (*yeguoAccess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client.mu.Lock()
	if access := client.access; access != nil && time.Since(access.loadedAt) < time.Hour {
		client.mu.Unlock()
		return access, nil
	}
	if pending := client.pending; pending != nil {
		client.mu.Unlock()
		select {
		case <-pending.done:
			if (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return client.configuration(ctx)
			}
			return pending.access, pending.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &yeguoAccessCall{done: make(chan struct{})}
	client.pending = pending
	client.mu.Unlock()

	access, err := client.discoverConfiguration(ctx)
	client.mu.Lock()
	if err == nil {
		client.access = access
	}
	pending.access, pending.err = access, err
	client.pending = nil
	close(pending.done)
	client.mu.Unlock()
	return access, err
}

func yeguoAPIBase(document *html.Node, configured string) (string, error) {
	base := strings.TrimRight(configured, "/")
	if base == "" {
		for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
			return node.Data == "script" && providerHTMLAttr(node, "id") == "__NUXT_DATA__"
		}) {
			if node.FirstChild == nil {
				continue
			}
			var table []json.RawMessage
			if json.Unmarshal([]byte(node.FirstChild.Data), &table) != nil || len(table) > 100000 {
				continue
			}
			for _, raw := range table {
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || fields["apiBaseURL"] == nil {
					continue
				}
				var reference int
				if json.Unmarshal(fields["apiBaseURL"], &reference) == nil && reference >= 0 && reference < len(table) {
					_ = json.Unmarshal(table[reference], &base)
				}
				if base != "" {
					break
				}
			}
		}
	}
	address, err := url.Parse(base)
	if err != nil || !isProviderHTTPMediaURL(base) || address.User != nil || address.RawQuery != "" || address.Fragment != "" {
		return "", errors.New("野果页面未提供有效的接口入口")
	}
	return strings.TrimRight(base, "/"), nil
}

func yeguoScriptURL(base, reference string) string {
	page, err := url.Parse(base)
	if err != nil {
		return ""
	}
	address, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	address = page.ResolveReference(address)
	if address.User != nil || providerMediaOrigin(address) != providerMediaOrigin(page) ||
		!strings.HasPrefix(address.Path, "/_nuxt/") || !strings.HasSuffix(address.Path, ".js") {
		return ""
	}
	address.Fragment = ""
	return address.String()
}

func yeguoPublicBytes(value string) []byte {
	if !strings.Contains(value, "_") {
		return []byte(value)
	}
	var decoded []byte
	for _, part := range strings.Split(value, "_") {
		number, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return nil
		}
		decoded = append(decoded, byte(number))
	}
	return decoded
}

func parseYeguoPublicConfiguration(script string) *yeguoAccess {
	fields := map[string]string{}
	for _, match := range yeguoPublicField.FindAllStringSubmatch(script, 32) {
		fields[match[1]] = match[2]
	}
	if fields["version"] != "v0" || fields["mode"] != "CBC" || fields["padding"] != "Pkcs7" {
		return nil
	}
	access := &yeguoAccess{key: yeguoPublicBytes(fields["key"]), iv: yeguoPublicBytes(fields["iv"]),
		signKey: yeguoPublicBytes(fields["sign_key"])}
	if (len(access.key) != 16 && len(access.key) != 24 && len(access.key) != 32) ||
		len(access.iv) != aes.BlockSize || len(access.signKey) == 0 || len(access.signKey) > 128 {
		return nil
	}
	return access
}

func (client *yeguoAPIClient) discoverConfiguration(ctx context.Context) (*yeguoAccess, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	d := client.downloader
	document, pageURL, err := d.fetchProviderPage(ctx, client.site+"/", client.site+"/", "")
	if err != nil {
		return nil, err
	}
	apiBase, err := yeguoAPIBase(document, d.cfg.YeguoAPIURL)
	if err != nil {
		return nil, err
	}
	entry := ""
	for _, script := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Data == "script" && providerHTMLAttr(node, "type") == "module"
	}) {
		if entry = yeguoScriptURL(pageURL, providerHTMLAttr(script, "src")); entry != "" {
			break
		}
	}
	if entry == "" {
		return nil, errors.New("野果页面未提供接口配置脚本")
	}
	entryBody, err := d.fetchProviderText(ctx, entry, pageURL)
	if err != nil {
		return nil, err
	}
	imports := yeguoModuleImport.FindAllStringSubmatch(entryBody, 64)
	sort.SliceStable(imports, func(i, j int) bool {
		return !strings.Contains(imports[i][1], ",") && strings.Contains(imports[j][1], ",")
	})
	seen := map[string]bool{}
	var lastErr error
	for _, imported := range imports {
		if len(imported[1]) > 2400 {
			continue
		}
		address := yeguoScriptURL(entry, imported[2])
		if address == "" || seen[address] {
			continue
		}
		if len(seen) >= 20 {
			break
		}
		seen[address] = true
		body, err := d.fetchProviderText(ctx, address, pageURL)
		if err != nil {
			lastErr = err
			var backoff *requestBackoff
			if errors.As(err, &backoff) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		access := parseYeguoPublicConfiguration(body)
		if access == nil {
			continue
		}
		var identifier [16]byte
		if _, err := rand.Read(identifier[:]); err != nil {
			return nil, errors.New("无法初始化野果请求会话")
		}
		access.base, access.identifier, access.loadedAt = apiBase, hex.EncodeToString(identifier[:]), time.Now()
		return access, nil
	}
	return nil, errors.Join(errors.New("野果接口配置已变化，暂时无法解码，请稍后重试"), lastErr)
}

func decodeYeguoJSONObject(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errYeguoDecode
	}
	return value, nil
}

func yeguoResponseSignature(value map[string]any, key []byte) (string, error) {
	var fields []string
	for name, field := range value {
		if name != "sign" && name != "_ver" && field != nil {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	var pieces []string
	for _, name := range fields {
		var field string
		switch value := value[name].(type) {
		case string:
			field = value
		case json.Number:
			field = value.String()
		case bool:
			field = strconv.FormatBool(value)
		default:
			return "", errYeguoDecode
		}
		if name == "data" {
			field = strings.ReplaceAll(field, " ", "+")
		}
		pieces = append(pieces, name+"="+field)
	}
	digest := sha256.Sum256(append([]byte(strings.Join(pieces, "&")), key...))
	signature := md5.Sum([]byte(hex.EncodeToString(digest[:])))
	return hex.EncodeToString(signature[:]), nil
}

func decodeYeguoResponse(body []byte, access *yeguoAccess) (map[string]any, error) {
	envelope, err := decodeYeguoJSONObject(body)
	if err != nil {
		return nil, err
	}
	if signature := mapString(envelope, "sign"); signature != "" {
		expected, err := yeguoResponseSignature(envelope, access.signKey)
		if err != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) != 1 {
			return nil, errYeguoDecode
		}
	}
	if encoded, encrypted := envelope["data"].(string); encrypted {
		ciphertext, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(encoded), " ", "+"))
		if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
			return nil, errYeguoDecode
		}
		block, err := aes.NewCipher(access.key)
		if err != nil {
			return nil, errYeguoDecode
		}
		plain := make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, access.iv).CryptBlocks(plain, ciphertext)
		plain, err = pkcs7Unpad(plain, aes.BlockSize)
		if err != nil {
			return nil, errYeguoDecode
		}
		return decodeYeguoJSONObject(plain)
	}
	return envelope, nil
}

func (client *yeguoAPIClient) call(ctx context.Context, route string, parameters url.Values) (map[string]any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		access, err := client.configuration(ctx)
		if err != nil {
			return nil, err
		}
		values := url.Values{"bundleId": {"com.pwa.mater"}, "version": {"1.3.2"}, "oauth_type": {"web"},
			"language": {"zh"}, "via": {"pwa"}, "oauth_id": {access.identifier}, "trace_id": {access.identifier}, "token": {""}}
		for key, value := range parameters {
			values[key] = append([]string(nil), value...)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, access.base+route, strings.NewReader(values.Encode()))
		if err != nil {
			return nil, errors.New("野果接口地址无效")
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json, text/plain, */*")
		request.Header.Set("Origin", client.site)
		request.Header.Set("Referer", client.site+"/")
		timeout := 15 * time.Second
		if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background {
			timeout = 8 * time.Second
		}
		response, err := client.downloader.doCatalogRequestWithTimeout(request, timeout)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 || catalogResponseBlockReason(response, body) != "" {
			return nil, client.downloader.catalogResponseError(request, response, body)
		}
		if readErr != nil || len(body) > 8<<20 {
			return nil, errors.New("野果接口数据过大或读取失败")
		}
		payload, err := decodeYeguoResponse(body, access)
		if errors.Is(err, errYeguoDecode) && attempt == 0 {
			client.mu.Lock()
			if client.access == access {
				client.access = nil
			}
			client.mu.Unlock()
			continue
		}
		if err != nil {
			return nil, err
		}
		if mapString(payload, "status") != "1" {
			if mapString(payload, "status") == "-1" {
				return nil, errors.New("野果当前内容需要站源授权")
			}
			return nil, fmt.Errorf("野果请求未完成：%s", firstNonEmpty(truncate(cleanText(mapString(payload, "msg")), 180), "请稍后重试"))
		}
		data, valid := payload["data"].(map[string]any)
		if !valid {
			return nil, errors.New("野果返回的数据格式无效")
		}
		return data, nil
	}
	return nil, errYeguoDecode
}
