package httpclient

import "net/url"

func BuildURL(
	baseURL, path string,
	query url.Values,
) (
	string,
	error,
) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}

	u = u.ResolveReference(rel)
	u.RawQuery = query.Encode()

	return u.String(), nil
}
