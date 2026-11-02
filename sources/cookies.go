package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Cookies is a name→value cookie jar for one site.
type Cookies map[string]string

// Header renders the jar as a Cookie header value, in a stable order.
func (c Cookies) Header() string {
	names := make([]string, 0, len(c))
	for k := range c {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+c[k])
	}
	return strings.Join(parts, "; ")
}

// CookieLoader runs the configured cookie command for a domain.
type CookieLoader func(ctx context.Context, domain string) (Cookies, error)

// CommandCookieLoader returns a loader that runs cmd through /bin/sh with
// "{domain}" substituted. Cookie values are never logged or echoed in errors.
func CommandCookieLoader(cmd string) CookieLoader {
	return func(ctx context.Context, domain string) (Cookies, error) {
		line := strings.ReplaceAll(cmd, "{domain}", shellQuote(domain))
		c := exec.CommandContext(ctx, "/bin/sh", "-c", line)
		var out, errb bytes.Buffer
		c.Stdout, c.Stderr = &out, &errb
		if err := c.Run(); err != nil {
			return nil, fmt.Errorf("cookie command failed for %s: %v", domain, err)
		}
		jar, err := ParseCookies(out.Bytes())
		if err != nil {
			return nil, fmt.Errorf("cookie command output for %s: %w", domain, err)
		}
		if len(jar) == 0 {
			return nil, fmt.Errorf("no cookies for %s (signed in, and allowed by your cookie tool?)", domain)
		}
		return jar, nil
	}
}

// ParseCookies accepts a JSON array of {name,value} (any key case), a
// "Cookie: a=b; c=d" line, or a bare "a=b; c=d" header.
func ParseCookies(data []byte) (Cookies, error) {
	data = bytes.TrimSpace(data)
	jar := Cookies{}
	if len(data) == 0 {
		return jar, nil
	}
	if data[0] == '[' {
		var items []map[string]interface{}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("not a JSON cookie array")
		}
		for _, it := range items {
			name, _ := pick(it, "name", "Name").(string)
			value, _ := pick(it, "value", "Value").(string)
			if name != "" {
				if _, seen := jar[name]; !seen {
					jar[name] = value
				}
			}
		}
		return jar, nil
	}
	s := string(data)
	if i := strings.Index(strings.ToLower(s), "cookie:"); i == 0 {
		s = s[len("cookie:"):]
	}
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			if _, seen := jar[kv[0]]; !seen {
				jar[kv[0]] = kv[1]
			}
		}
	}
	return jar, nil
}

func pick(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
