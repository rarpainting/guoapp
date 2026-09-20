package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type providerFallbackTransport func(*http.Request) (*http.Response, error)

func (transport providerFallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestHuangguoFallbackKeepsPlaybackOnSuccessfulHost(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("黄果播放闭环仅在全站源构建中启用")
	}
	for _, fixture := range []struct {
		name       string
		source     string
		catalog    string
		detailPath string
		pagePath   string
	}{
		{
			name:       "ai",
			source:     sourceHuangguoAI,
			catalog:    `{"data":{"items":[{"id":"ai-1","title":"AI 合成短剧"}]}}`,
			detailPath: "/detail/ai-1/",
			pagePath:   "/video/ai-1/ep-1",
		},
		{
			name:       "video",
			source:     sourceHuangguoVideo,
			catalog:    `<article class="video-card"><a href="/series/video-1" title="视频合成短剧">视频合成短剧</a></article>`,
			detailPath: "/series/video-1",
			pagePath:   "/video/video-1-ep-1",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/videos/category/ai-duanju":
					if fixture.source != sourceHuangguoAI {
						writer.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = io.WriteString(writer, fixture.catalog)
				case "/videos":
					if fixture.source != sourceHuangguoVideo {
						writer.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = io.WriteString(writer, fixture.catalog)
				case fixture.detailPath:
					if fixture.source == sourceHuangguoAI {
						_, _ = io.WriteString(writer, `<title>AI 合成短剧</title><a href="`+fixture.pagePath+`">第 1 集</a>`)
					} else {
						_, _ = io.WriteString(writer, `<title>视频合成短剧</title><article class="video-card"><a href="`+fixture.pagePath+`" title="第 1 集">第 1 集</a></article>`)
					}
				case fixture.pagePath:
					if fixture.source == sourceHuangguoAI {
						_, _ = io.WriteString(writer, `<div data-play-src="https://huangguoai.com/media/index.m3u8"></div>`)
					} else {
						_, _ = io.WriteString(writer, `<div data-hls="https://huangguo.video/media/index.m3u8"></div>`)
					}
				case "/media/index.m3u8":
					writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					_, _ = io.WriteString(writer, "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:1,\nsegment.ts\n#EXT-X-ENDLIST\n")
				case "/media/segment.ts":
					writer.Header().Set("Content-Type", "video/mp2t")
					_, _ = io.WriteString(writer, "synthetic-segment")
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
			engine.downloader.cfg.Retries = 1
			engine.downloader.limiter = newRequestLimiter(3, 0)
			if fixture.source == sourceHuangguoAI {
				engine.downloader.cfg.HuangguoAIURL = upstream.URL
			} else {
				engine.downloader.cfg.HuangguoVideoURL = upstream.URL
			}
			base := http.DefaultTransport.(*http.Transport).Clone()
			engine.downloader.client = &http.Client{Timeout: 5 * time.Second, Transport: providerFallbackTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Hostname() == "huangguoai.com" || request.URL.Hostname() == "huangguo.video" {
					return nil, errors.New("playback escaped the successful provider host")
				}
				return base.RoundTrip(request)
			})}

			catalog, err := engine.nativeCatalog(context.Background(), nativeInput{Source: fixture.source, Page: 1})
			if err != nil || len(catalog.Items) != 1 {
				t.Fatalf("catalog: %+v, %v", catalog, err)
			}
			detail, err := engine.nativeDetail(context.Background(), catalog.Items[0])
			if err != nil {
				t.Fatal(err)
			}
			chapters := detail.(map[string]any)["chapters"].([]Chapter)
			if len(chapters) != 1 {
				t.Fatalf("chapters: %+v", chapters)
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
				t.Fatalf("playback escaped fallback host: status=%d body=%q error=%v", response.StatusCode, body, err)
			}
			segment := ""
			for _, line := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(line, "http://127.0.0.1:") {
					segment = line
					break
				}
			}
			if segment == "" {
				t.Fatalf("playlist did not proxy its media segment: %q", body)
			}
			response, err = http.Get(segment)
			if err != nil {
				t.Fatal(err)
			}
			segmentBody, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK || string(segmentBody) != "synthetic-segment" {
				t.Fatalf("segment escaped fallback host: status=%d body=%q error=%v", response.StatusCode, segmentBody, readErr)
			}
		})
	}
}
