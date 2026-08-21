package webapi

import (
	"fmt"
	"net/url"
	"strings"
)

type URL struct {
	value *url.URL
}

func ParseURL(input string, base *string) (URL, error) {
	reference, err := url.Parse(input)
	if err != nil {
		return URL{}, err
	}
	if base != nil {
		baseURL, err := url.Parse(*base)
		if err != nil || !baseURL.IsAbs() {
			return URL{}, fmt.Errorf("invalid base URL %q", *base)
		}
		reference = baseURL.ResolveReference(reference)
	}
	if !reference.IsAbs() || reference.Scheme == "" {
		return URL{}, fmt.Errorf("URL is not absolute")
	}
	return URL{value: reference}, nil
}

func (value URL) String() string {
	if value.value == nil {
		return ""
	}
	return value.value.String()
}

func (value URL) Href() string     { return value.String() }
func (value URL) Protocol() string { return value.value.Scheme + ":" }
func (value URL) Username() string {
	if value.value.User == nil {
		return ""
	}
	return value.value.User.Username()
}
func (value URL) Password() string {
	if value.value.User == nil {
		return ""
	}
	password, _ := value.value.User.Password()
	return password
}
func (value URL) Host() string     { return value.value.Host }
func (value URL) Hostname() string { return value.value.Hostname() }
func (value URL) Port() string     { return value.value.Port() }
func (value URL) Pathname() string {
	if value.value.Path == "" {
		return "/"
	}
	return value.value.EscapedPath()
}
func (value URL) Search() string {
	if value.value.RawQuery == "" && !value.value.ForceQuery {
		return ""
	}
	return "?" + value.value.RawQuery
}
func (value URL) Hash() string {
	if value.value.Fragment == "" {
		return ""
	}
	return "#" + value.value.EscapedFragment()
}
func (value URL) Origin() string {
	scheme := strings.ToLower(value.value.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "ws" && scheme != "wss" {
		return "null"
	}
	return scheme + "://" + value.value.Host
}
func (value URL) SearchParams() URLSearchParams { return ParseURLSearchParams(value.Search()) }

func (value *URL) SetHref(input string) error {
	parsed, err := ParseURL(input, nil)
	if err == nil {
		value.value = parsed.value
	}
	return err
}

func (value *URL) SetProtocol(protocol string) {
	value.value.Scheme = strings.TrimSuffix(protocol, ":")
}
func (value *URL) SetUsername(username string) {
	password := value.Password()
	if password == "" {
		value.value.User = url.User(username)
	} else {
		value.value.User = url.UserPassword(username, password)
	}
}
func (value *URL) SetPassword(password string) {
	value.value.User = url.UserPassword(value.Username(), password)
}
func (value *URL) SetHost(host string) { value.value.Host = host }
func (value *URL) SetHostname(hostname string) {
	port := value.Port()
	value.value.Host = hostname
	if port != "" {
		value.value.Host += ":" + port
	}
}
func (value *URL) SetPort(port string) {
	hostname := value.Hostname()
	value.value.Host = hostname
	if port != "" {
		value.value.Host += ":" + port
	}
}
func (value *URL) SetPathname(pathname string) { value.value.Path = pathname; value.value.RawPath = "" }
func (value *URL) SetSearch(search string) {
	value.value.RawQuery = strings.TrimPrefix(search, "?")
	value.value.ForceQuery = search == "?"
}
func (value *URL) SetHash(hash string) {
	value.value.Fragment = strings.TrimPrefix(hash, "#")
	value.value.RawFragment = ""
}
