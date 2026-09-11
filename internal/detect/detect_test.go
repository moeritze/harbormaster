package detect_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moeritze/harbormaster/internal/detect"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		cmd     string
		class   detect.Class
		ports   []int
		pids    []int
		wrapped bool
	}{
		// kill class
		{"kill 1234", detect.Kill, nil, []int{1234}, false},
		{"kill -9 1234", detect.Kill, nil, []int{1234}, false},
		{"kill -TERM 1234 5678", detect.Kill, nil, []int{1234, 5678}, false},
		{"kill -- -1234", detect.Kill, nil, []int{1234}, false},
		{"pkill -f next", detect.Kill, nil, nil, false},
		{"pkill node", detect.Kill, nil, nil, false},
		{"killall node", detect.Kill, nil, nil, false},
		{"fuser -k 3000/tcp", detect.Kill, []int{3000}, nil, false},
		{"lsof -ti:3000 | xargs kill", detect.Kill, []int{3000}, nil, false},
		{"lsof -ti :3000 | xargs kill -9", detect.Kill, []int{3000}, nil, false},
		{"lsof -t -i tcp:3000 | xargs kill", detect.Kill, []int{3000}, nil, false},
		{"kill $(lsof -ti:3000)", detect.Kill, []int{3000}, nil, false},
		{"kill -9 $(lsof -t -i:3000)", detect.Kill, []int{3000}, nil, false},
		{"npx kill-port 3000", detect.Kill, []int{3000}, nil, false},
		{"npx kill-port 3000 3001", detect.Kill, []int{3000, 3001}, nil, false},
		{"kill-port 8080", detect.Kill, []int{8080}, nil, false},
		{"cd app && lsof -ti:5173 | xargs kill", detect.Kill, []int{5173}, nil, false},
		{"pkill -f 'vite --port 5173'", detect.Kill, []int{5173}, nil, false},
		{"kill -s TERM 123", detect.Kill, nil, []int{123}, false},
		{"hm kill 3000 && kill -9 1234", detect.Kill, []int{3000}, []int{1234}, false},
		{"hm ls; pkill -f node", detect.Kill, nil, nil, false},
		// Only harbormaster's own subcommands are blanked out: "hm x" is
		// some other program (or a typo), and the kill after it is real.
		{"hm x lsof -ti:3100 | xargs kill", detect.Kill, []int{3100}, nil, false},
		// server start, unwrapped
		{"npm run dev", detect.ServerStart, nil, nil, false},
		{"npm run dev -- --port 3001", detect.ServerStart, []int{3001}, nil, false},
		{"npm start", detect.ServerStart, nil, nil, false},
		{"pnpm dev", detect.ServerStart, nil, nil, false},
		{"pnpm run dev --port=4000", detect.ServerStart, []int{4000}, nil, false},
		{"yarn dev", detect.ServerStart, nil, nil, false},
		{"bun run dev", detect.ServerStart, nil, nil, false},
		{"npx next dev -p 3005", detect.ServerStart, []int{3005}, nil, false},
		{"next dev", detect.ServerStart, nil, nil, false},
		{"vite", detect.ServerStart, nil, nil, false},
		{"npx vite --port 5174 --host", detect.ServerStart, []int{5174}, nil, false},
		{"vite preview", detect.ServerStart, nil, nil, false},
		{"astro dev", detect.ServerStart, nil, nil, false},
		{"nuxt dev", detect.ServerStart, nil, nil, false},
		{"remix dev", detect.ServerStart, nil, nil, false},
		{"ng serve --port 4300", detect.ServerStart, []int{4300}, nil, false},
		{"python3 -m http.server 8000", detect.ServerStart, []int{8000}, nil, false},
		{"python -m http.server", detect.ServerStart, nil, nil, false},
		{"uvicorn app:main --port 8001 --reload", detect.ServerStart, []int{8001}, nil, false},
		{"flask run --port 5001", detect.ServerStart, []int{5001}, nil, false},
		{"rails s -p 3002", detect.ServerStart, []int{3002}, nil, false},
		{"bundle exec rails server", detect.ServerStart, nil, nil, false},
		{"php artisan serve --port=8081", detect.ServerStart, []int{8081}, nil, false},
		{"hugo server -p 1313", detect.ServerStart, []int{1313}, nil, false},
		{"PORT=3010 npm run dev", detect.ServerStart, []int{3010}, nil, false},
		{"cd web && PORT=3011 npm run dev &", detect.ServerStart, []int{3011}, nil, false},
		{"go run ./cmd/api --port 9000", detect.ServerStart, []int{9000}, nil, false},
		{"cargo run -- --port 9001", detect.ServerStart, []int{9001}, nil, false},
		{"npm run dev > dev.log 2>&1 &", detect.ServerStart, nil, nil, false},
		// wrapped
		{"hm run --label x -- npm run dev", detect.ServerStart, nil, nil, true},
		{"hm run -- npm run dev", detect.ServerStart, nil, nil, true},
		{"hm run -- kill 1234", detect.Kill, nil, []int{1234}, true},
		{"hm run --label x -- npm run dev", detect.ServerStart, nil, nil, true},
		{"hm run npm run dev", detect.ServerStart, nil, nil, true},
		{"harbormaster run -- npm run dev", detect.ServerStart, nil, nil, true},
		{"harbormaster run --port 3000 -- vite", detect.ServerStart, []int{3000}, nil, true},
		// none
		{"npm run build", detect.None, nil, nil, false},
		{"npm test", detect.None, nil, nil, false},
		{"vite build", detect.None, nil, nil, false},
		{"git status", detect.None, nil, nil, false},
		{"ls -la", detect.None, nil, nil, false},
		{"curl http://localhost:3000/health", detect.None, []int{3000}, nil, false},
		{"echo skill", detect.None, nil, nil, false},
		{"hm ls", detect.None, nil, nil, false},
		{"hm kill 3000", detect.None, []int{3000}, nil, false},
		{"docker compose up", detect.None, nil, nil, false},
		{"mkdir -p 3000", detect.None, nil, nil, false},
		{"docker run -p 8080:80 nginx", detect.None, nil, nil, false},
		{"", detect.None, nil, nil, false},
	}
	for _, c := range cases {
		got := detect.Classify(c.cmd)
		if got.Class != c.class {
			t.Errorf("%q: class %v, want %v (matched %q)", c.cmd, got.Class, c.class, got.Matched)
		}
		if !reflect.DeepEqual(got.Ports, c.ports) {
			t.Errorf("%q: ports %v, want %v", c.cmd, got.Ports, c.ports)
		}
		if !reflect.DeepEqual(got.Pids, c.pids) {
			t.Errorf("%q: pids %v, want %v", c.cmd, got.Pids, c.pids)
		}
		if got.Wrapped != c.wrapped {
			t.Errorf("%q: wrapped %v, want %v", c.cmd, got.Wrapped, c.wrapped)
		}
	}
}

// TestClassifyCapsScannedInput: Classify runs on the PreToolUse hot path,
// before every shell command. A generated one-liner or an inlined heredoc
// can be megabytes long; a dozen regexes over all of it would show up as
// latency on every command, so only the first 16 KiB is inspected -- which
// is still far past where any of these patterns can match.
func TestClassifyCapsScannedInput(t *testing.T) {
	// A command exactly at the scan cap is the baseline; one six times
	// larger must cost about the same, because Classify never looks past
	// the cap. Comparing against a baseline measured in the same process
	// keeps the assertion meaningful on slow or loaded machines.
	capped := "npm run dev " + strings.Repeat("#", 16*1024-12)
	large := "npm run dev " + strings.Repeat("#", 100*1024)
	const iterations = 20
	avg := func(cmd string) time.Duration {
		start := time.Now()
		for range iterations {
			if got := detect.Classify(cmd); got.Class != detect.ServerStart {
				t.Fatalf("class %v, want server_start", got.Class)
			}
		}
		return time.Since(start) / iterations
	}
	base := avg(capped)
	big := avg(large)
	slack := 10 * time.Millisecond * raceBudget
	if big > 2*base+slack {
		t.Fatalf("Classify on 100 KB averaged %s vs %s at the 16 KB cap; the input cap is not working", big, base)
	}
}

func TestClassifyNeverPanicsOnGarbage(t *testing.T) {
	for _, s := range []string{"\x00\x01", "kill -9", "lsof -ti:", "--port=", "PORT=abc npm run dev", "kill 99999999999999999999"} {
		func(s string) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Classify(%q) panicked: %v", s, r)
				}
			}()
			_ = detect.Classify(s)
		}(s)
	}
}
