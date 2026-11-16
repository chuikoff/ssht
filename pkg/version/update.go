package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const repo = "chuikoff/ssht"

// IsNewer reports whether latest is a higher dotted version than current.
// A leading "v" is ignored. Non-numeric tails are ignored.
func IsNewer(latest, current string) bool {
	l := parts(latest)
	c := parts(current)
	n := len(l)
	if len(c) > n {
		n = len(c)
	}
	for i := 0; i < n; i++ {
		lv, cv := 0, 0
		if i < len(l) {
			lv = l[i]
		}
		if i < len(c) {
			cv = c[i]
		}
		if lv != cv {
			return lv > cv
		}
	}
	return false
}

func parts(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil
	}
	raw := strings.Split(v, ".")
	out := make([]int, len(raw))
	for i, p := range raw {
		j := 0
		for j < len(p) && p[j] >= '0' && p[j] <= '9' {
			j++
		}
		if j == 0 {
			continue
		}
		n, err := strconv.Atoi(p[:j])
		if err == nil {
			out[i] = n
		}
	}
	return out
}

// LatestRelease returns the newest GitHub release tag without a leading "v".
// A network or API failure returns an error and the caller stays quiet.
func LatestRelease() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ssht")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: %s", resp.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	tag := strings.TrimPrefix(strings.TrimSpace(body.TagName), "v")
	if tag == "" {
		return "", fmt.Errorf("github: empty tag")
	}
	return tag, nil
}
