package session

import (
	"net/http"
	"net/url"
	"sort"
)

type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func FromHTTPCookie(cookie *http.Cookie) Cookie {
	return Cookie{
		Name:  cookie.Name,
		Value: cookie.Value,
	}
}

func (c Cookie) ToHTTPCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:   c.Name,
		Value:  c.Value,
		Path:   "/",
		Secure: secure,
	}
}

func RestoreCookies(
	jar http.CookieJar,
	target *url.URL,
	cookies []Cookie,
) {
	if jar == nil || target == nil {
		return
	}

	values := make(
		[]*http.Cookie,
		0,
		len(cookies),
	)

	for _, cookie := range cookies {
		if cookie.Name == "" || cookie.Value == "" {
			continue
		}
		values = append(
			values,
			cookie.ToHTTPCookie(target.Scheme == "https"),
		)
	}

	jar.SetCookies(
		target,
		values,
	)
}

func SnapshotCookies(
	jar http.CookieJar,
	target *url.URL,
) []Cookie {
	if jar == nil || target == nil {
		return nil
	}

	httpCookies := jar.Cookies(target)

	result := make(
		[]Cookie,
		0,
		len(httpCookies),
	)

	for _, cookie := range httpCookies {
		if cookie.Name == "" || cookie.Value == "" {
			continue
		}
		result = append(
			result,
			FromHTTPCookie(cookie),
		)
	}

	sort.Slice(
		result,
		func(i, j int) bool {
			return result[i].Name < result[j].Name
		},
	)

	return result
}
