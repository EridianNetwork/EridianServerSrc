package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var viewerHTML string

type LogEntry struct {
	ID       int    `json:"id"`
	Priority string `json:"priority"`
	Category string `json:"category"`
	Time     string `json:"time"`
	Text     string `json:"text"`
	Stack    string `json:"stack,omitempty"`
}

var (
	rePTag      = regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`)
	rePriSpan   = regexp.MustCompile(`<span class="(Log|Warning|Error|Assert|Exception)"`)
	reTimeSpan  = regexp.MustCompile(`<span class="time">([^<]+)</span>`)
	reAfterSTK  = regexp.MustCompile(`(?s)>STACK</a>(.*)$`)
	reStripTags = regexp.MustCompile(`<[^>]+>`)
	reCatPrefix = regexp.MustCompile(`^\[(\w+)\]\s*`)
	rePreTag    = regexp.MustCompile(`(?s)<pre[^>]*>(.*?)</pre>`)
	reTraceID   = regexp.MustCompile(`hide\('(trace\d+)'\)`)
)

func parseChunk(data string) []LogEntry {
	stacks := map[string]string{}
	for _, m := range rePreTag.FindAllStringSubmatch(data, -1) {
		content := strings.TrimSpace(reStripTags.ReplaceAllString(m[1], ""))
		if content != "" {
			idMatch := regexp.MustCompile(`id="([^"]+)"`).FindStringSubmatch(m[0])
			if idMatch != nil {
				stacks[idMatch[1]] = content
			}
		}
	}

	var entries []LogEntry
	for _, m := range rePTag.FindAllStringSubmatch(data, -1) {
		inner := m[1]

		pm := rePriSpan.FindStringSubmatch(inner)
		if pm == nil {
			continue
		}
		priority := pm[1]

		timeStr := ""
		if tm := reTimeSpan.FindStringSubmatch(inner); tm != nil {
			timeStr = tm[1]
		}

		text := ""
		if sm := reAfterSTK.FindStringSubmatch(inner); sm != nil {
			text = strings.TrimSpace(reStripTags.ReplaceAllString(sm[1], ""))
		} else {
			text = strings.TrimSpace(reStripTags.ReplaceAllString(inner, ""))
		}
		text = html.UnescapeString(text)

		category := ""
		if cm := reCatPrefix.FindStringSubmatch(text); cm != nil {
			category = cm[1]
			text = strings.TrimSpace(text[len(cm[0]):])
		}

		if text == "" {
			continue
		}

		stack := ""
		if tm := reTraceID.FindStringSubmatch(inner); tm != nil {
			stack = stacks[tm[1]]
		}

		entries = append(entries, LogEntry{
			Priority: priority,
			Category: category,
			Time:     timeStr,
			Text:     text,
			Stack:    stack,
		})
	}
	return entries
}

type Hub struct {
	mu      sync.Mutex
	entries []LogEntry
	idSeq   atomic.Int64
	subs    map[chan LogEntry]struct{}
}

var hub = &Hub{subs: make(map[chan LogEntry]struct{})}

func (h *Hub) publish(entries []LogEntry) {
	if len(entries) == 0 {
		return
	}
	h.mu.Lock()
	for i := range entries {
		entries[i].ID = int(h.idSeq.Add(1))
	}
	h.entries = append(h.entries, entries...)
	subs := make([]chan LogEntry, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		for _, e := range entries {
			select {
			case ch <- e:
			default:
			}
		}
	}
}

func (h *Hub) subscribe() ([]LogEntry, chan LogEntry) {
	ch := make(chan LogEntry, 1024)
	h.mu.Lock()
	snapshot := make([]LogEntry, len(h.entries))
	copy(snapshot, h.entries)
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return snapshot, ch
}

func (h *Hub) unsubscribe(ch chan LogEntry) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}

var archiveReplacer = strings.NewReplacer(
	"http://sghw-badass2.2kgames.t2.corp/debug/log.js",  "log.js",
	"http://sghw-badass2.2kgames.t2.corp/debug/log.css", "log.css",
)

var connSeq atomic.Int64

var archiveFilenameChars = strings.NewReplacer(":", "-", "[", "", "]", "")

func handleTCP(conn net.Conn, outDir string) {
	defer conn.Close()
	log.Printf("[LOGSVR] game connected from %s", conn.RemoteAddr())

	seq := connSeq.Add(1)
	archivePath := filepath.Join(outDir, fmt.Sprintf("bol_output_%03d_%s_%s.html",
		seq, time.Now().Format("20060102_150405"), archiveFilenameChars.Replace(conn.RemoteAddr().String())))

	var archive *os.File
	if outDir != "" {
		_ = os.MkdirAll(outDir, 0o755)
		if f, err := os.Create(archivePath); err == nil {
			_, _ = fmt.Fprintf(f, "<!DOCTYPE html><html><head><meta charset='utf-8'></head><body>\n")
			archive = f
			log.Printf("[LOGSVR] archiving connection to %s", archivePath)
		} else {
			log.Printf("[LOGSVR] could not create archive %s: %v", archivePath, err)
		}
	}
	defer func() {
		if archive != nil {
			_, _ = fmt.Fprintf(archive, "\n</body></html>")
			_ = archive.Close()
		}
	}()

	buf := make([]byte, 8192)
	var pending strings.Builder

	for {
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])

			if archive != nil {
				_, _ = fmt.Fprint(archive, archiveReplacer.Replace(chunk))
				_ = archive.Sync()
			}

			pending.WriteString(chunk)

			if strings.Contains(chunk, "</p>") {
				full := pending.String()
				entries := parseChunk(full)
				hub.publish(entries)

				if idx := strings.LastIndex(full, "</p>"); idx >= 0 {
					tail := full[idx+4:]
					pending.Reset()
					pending.WriteString(tail)
				}
			}
		}
		if err != nil {
			break
		}
	}
	log.Printf("[LOGSVR] game disconnected from %s", conn.RemoteAddr())
}

func serveSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}

	snapshot, ch := hub.subscribe()
	defer hub.unsubscribe(ch)

	for _, e := range snapshot {
		data, _ := json.Marshal(e)
		_, _ = fmt.Fprintf(w, "event: entry\ndata: %s\n\n", data)
	}
	flusher.Flush()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(e)
			_, _ = fmt.Fprintf(w, "event: entry\ndata: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

const legacyJS = `
function hide_class(cls){var e=document.getElementsByClassName(cls);for(var i=0;i<e.length;i++){if(e[i].classList.contains('Button'))continue;e[i].style.display=(e[i].style.display==='none')?'':'none';}}
function hide_priority(p){hide_class(p);}
function hide(id){var e=document.getElementById(id);if(e)e.style.display=(e.style.display==='none')?'':'none';}
`
const legacyCSS = `
body{background:#1a1a1a;color:#ddd;font-family:monospace;font-size:12px;}
p{margin:1px 0;padding:2px 4px;border-left:3px solid #444;}
.Error{border-color:#e55;background:#2a0a0a;color:#f88;}
.Warning{border-color:#ea0;background:#2a1f00;color:#fd8;}
.Assert{border-color:#f80;background:#2a1500;color:#fc8;}
.Exception{border-color:#f0f;background:#1a002a;color:#e8e;}
`

func main() {
	tcpPort  := flag.Int("tcp-port",  666, "TCP port for game log stream")
	httpPort := flag.Int("http-port",  80, "HTTP port for live viewer and assets")
	outDir   := flag.String("out-dir", "logs", "directory for per-connection bol_output_*.html archives and assets")
	flag.Parse()

	_ = os.MkdirAll(*outDir, 0o755)
	_ = os.WriteFile(filepath.Join(*outDir, "log.js"),  []byte(legacyJS),  0o644)
	_ = os.WriteFile(filepath.Join(*outDir, "log.css"), []byte(legacyCSS), 0o644)

	mux := http.NewServeMux()
	mux.HandleFunc("/events", serveSSE)

	assetHandler := func(ct, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			_, _ = io.WriteString(w, body)
		}
	}
	mux.HandleFunc("/log.js",        assetHandler("application/javascript", legacyJS))
	mux.HandleFunc("/debug/log.js",  assetHandler("application/javascript", legacyJS))
	mux.HandleFunc("/log.css",       assetHandler("text/css", legacyCSS))
	mux.HandleFunc("/debug/log.css", assetHandler("text/css", legacyCSS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, viewerHTML)
	})

	httpAddr := fmt.Sprintf("0.0.0.0:%d", *httpPort)
	go func() {
		if err := http.ListenAndServe(httpAddr, mux); err != nil {
			log.Printf("[LOGSVR] HTTP on %s: %v (need admin for port 80?)", httpAddr, err)
		}
	}()

	tcpAddr := fmt.Sprintf("0.0.0.0:%d", *tcpPort)
	ln, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("[LOGSVR] TCP listen on %s: %v", tcpAddr, err)
	}

	fmt.Println(strings.Repeat("═", 56))
	fmt.Println("  BOL Log Server. Unity 4.5.4f1")
	fmt.Printf("  TCP  :%d  ← game connects here\n", *tcpPort)
	fmt.Printf("  HTTP :%d   ← open http://localhost/ in browser\n", *httpPort)
	fmt.Printf("  Archive → %s/bol_output_<seq>_<time>_<addr>.html (one file per connection)\n", *outDir)
	fmt.Println(strings.Repeat("═", 56))
	fmt.Println()
	fmt.Println("  hosts entry (run as admin):")
	fmt.Println("    127.0.0.1  sghw-badass2.2kgames.t2.corp")
	fmt.Println()

	log.Printf("[LOGSVR] HTTP ready on %s", httpAddr)
	log.Printf("[LOGSVR] TCP  ready on %s", tcpAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[LOGSVR] accept: %v", err)
			continue
		}
		go handleTCP(conn, *outDir)
	}
}
