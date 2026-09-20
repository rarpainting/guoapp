package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func huangdouFixtureRequest(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	if request.Header.Get("version") != huangdouVersion || request.Header.Get("deviceType") != huangdouDeviceType || request.Header.Get("requestId") == "" || request.Header.Get("sign") == "" {
		t.Fatalf("黄豆请求缺少协议头: %v", request.Header)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || len(body) < 32 {
		t.Fatalf("黄豆请求体无效: %d, %v", len(body), err)
	}
	key, err := huangdouKey(request.Header.Get("requestId"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aesCBCDecrypt(body[16:], key, body[:16])
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("黄豆请求解压失败: %v, %v", err, closeErr)
	}
	var envelope map[string]any
	if err := json.Unmarshal(decoded, &envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		t.Fatalf("黄豆请求缺少 data: %s", decoded)
	}
	return data
}

func TestHuangdouNativeCatalogDetailAndPlayback(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("黄豆播放闭环仅在全站源构建中启用")
	}
	const dramaID = "huangdou-fixture-1"
	var upstream *httptest.Server
	upstream = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/drama/list":
			data := huangdouFixtureRequest(t, request)
			if data["page"] != "1" || data["page_size"] != "30" {
				t.Fatalf("黄豆目录参数错误: %+v", data)
			}
			_, _ = io.WriteString(writer, `{"data":{"list":[{"id":"`+dramaID+`","name":"黄豆合成短剧","episode_count":"2","is_vip":false}]}}`)
		case "/api/drama/detail":
			data := huangdouFixtureRequest(t, request)
			if data["id"] != dramaID {
				t.Fatalf("黄豆详情参数错误: %+v", data)
			}
			_, _ = io.WriteString(writer, `{"data":{"id":"`+dramaID+`","name":"黄豆合成短剧","episode_count":"2","episodes":[{"seq":"2","name":"第 2 集"},{"seq":"1","name":"第 1 集","is_vip":true}]}}`)
		case "/api/drama/play":
			data := huangdouFixtureRequest(t, request)
			if data["id"] != dramaID || data["seq"] != "1" {
				t.Fatalf("黄豆播放参数错误: %+v", data)
			}
			_, _ = io.WriteString(writer, `{"data":{"m3u8":"`+upstream.URL+`/media/index.m3u8","duration":"1"}}`)
		case "/media/index.m3u8":
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(writer, "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:1,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/media/segment.ts":
			writer.Header().Set("Content-Type", "video/mp2t")
			_, _ = io.WriteString(writer, "huangdou-segment")
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.cfg.HuangdouURL = upstream.URL
	engine.downloader.cfg.Retries = 1
	engine.downloader.limiter = newRequestLimiter(3, 0)
	engine.downloader.client = upstream.Client()
	catalog, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHuangdou, Page: 1})
	if err != nil || len(catalog.Items) != 1 || catalog.Items[0].ID != sourceHuangdou+":"+dramaID {
		t.Fatalf("catalog: %+v, %v", catalog, err)
	}
	detail, err := engine.nativeDetail(context.Background(), catalog.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	chapters := detail.(map[string]any)["chapters"].([]Chapter)
	if len(chapters) != 2 || chapters[0].Title != "第 1 集" || !chapters[0].VIP || chapters[1].Title != "第 2 集" {
		t.Fatalf("黄豆分集未排序或 VIP 状态错误: %+v", chapters)
	}
	plan, err := engine.nativeResolve(context.Background(), nativeInput{Drama: catalog.Items[0], Chapter: chapters[0], Index: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.nativeReleasePlayback(plan.Session)
	defer engine.stream.server.Close()
	response, err := http.Get(plan.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "#EXTM3U") {
		t.Fatalf("黄豆播放清单错误: status=%d body=%q error=%v", response.StatusCode, body, err)
	}
	segment := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "http://127.0.0.1:") {
			segment = line
			break
		}
	}
	if segment == "" {
		t.Fatalf("黄豆播放清单未代理分片: %q", body)
	}
	response, err = http.Get(segment)
	if err != nil {
		t.Fatal(err)
	}
	segmentBody, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || string(segmentBody) != "huangdou-segment" {
		t.Fatalf("黄豆播放分片错误: status=%d body=%q error=%v", response.StatusCode, segmentBody, readErr)
	}
}
