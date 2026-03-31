package openai

import (
	"io"
	"net/http"
	"strings"
)

type handler struct {
	gw Gateway
}

func (h *handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if h.gw == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("gateway not configured"))
		return
	}

	upstreamResp, err := h.gw.Proxy(r)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if upstreamResp == nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("empty upstream response"))
		return
	}
	defer upstreamResp.Body.Close()

	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(upstreamResp.StatusCode)

	if strings.Contains(upstreamResp.Header.Get("Content-Type"), "text/event-stream") {
		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := upstreamResp.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				break
			}
		}
		return
	}

	_, _ = io.Copy(w, upstreamResp.Body)
}

func (h *handler) embeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if h.gw == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("gateway not configured"))
		return
	}

	upstreamResp, err := h.gw.Proxy(r)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if upstreamResp == nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("empty upstream response"))
		return
	}
	defer upstreamResp.Body.Close()

	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = io.Copy(w, upstreamResp.Body)
}

func (h *handler) responses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if h.gw == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("gateway not configured"))
		return
	}

	upstreamResp, err := h.gw.Proxy(r)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if upstreamResp == nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("empty upstream response"))
		return
	}
	defer upstreamResp.Body.Close()

	for k, vv := range upstreamResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = io.Copy(w, upstreamResp.Body)
}
