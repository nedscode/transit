package connect

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parameters are connection parameters that define the way the connection should be established.
type Parameters struct {
	TokenType   string
	Peers       []string
	Token       string
	TLSMode     int
	CertFile    string
	KeyFile     string
	Certificate tls.Certificate
	LeaderOnly  bool

	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	RetryCount     int
	PoolSize       int
	RequireType    string
}

// NewParams will create a new set of connection parameters with sane defaults.
func NewParams() *Parameters {
	return &Parameters{
		TokenType:      "transit",
		TLSMode:        1,
		LeaderOnly:     true,
		ConnectTimeout: defaultConnectTimeout,
		ReadTimeout:    defaultReadTimeout,
		RetryCount:     defaultRetries,
		RequireType:    "master",
	}
}

// URI will convert the connection parameters into a URI that can be later decoded via `ParseURI`.
func (c *Parameters) URI() string {
	var query []string
	parts := []string{c.TokenType, "://", strings.Join(c.Peers, ","), "/", c.Token}

	switch c.TLSMode {
	case 2:
		query = append(query, "tls=client")
		query = append(query, "cert="+url.QueryEscape(c.CertFile))
		query = append(query, "key="+url.QueryEscape(c.KeyFile))
	case 0:
		query = append(query, "tls=0")
	}

	if !c.LeaderOnly {
		query = append(query, "leader=0")
	}

	if c.ConnectTimeout != defaultConnectTimeout {
		query = append(query, "timeout="+renderDuration(c.ConnectTimeout))
	}
	if c.ReadTimeout != defaultReadTimeout {
		query = append(query, "read-timeout="+renderDuration(c.ReadTimeout))
	}
	if c.RetryCount != defaultRetries {
		query = append(query, fmt.Sprintf("retries=%d", c.RetryCount))
	}

	if len(query) > 0 {
		parts = append(parts, "?", strings.Join(query, "&"))
	}

	return strings.Join(parts, "")
}

// ParseURI will parse a transit URI into a set of connection parameters you provide (or a new set if you pass nil).
func ParseURI(uri string, params *Parameters) (*Parameters, error) {
	if params == nil {
		params = NewParams()
	}

	u, err := url.Parse(uri)

	if err != nil || u.Scheme != "transit" || u.Host == "" || u.Path == "" || u.Path == "/" {
		return nil, ErrInvalidURI
	}

	params.Peers = strings.Split(u.Host, ",")
	params.Token = strings.TrimLeft(u.Path, "/")
	if len(params.Token) == 48 {
		params.TokenType = "cluster"
	} else if len(params.Token) == 33 && params.Token[12] == '-' {
		params.TokenType = "master"
	} else {
		return nil, ErrInvalidToken
	}

	q := u.Query()
	switch q.Get("tls") {
	case "client":
		var cert, key []byte
		certFile := q.Get("cert")
		cert, err = ioutil.ReadFile(certFile)
		if err != nil || len(cert) == 0 {
			return nil, ErrInvalidCertFile
		}

		keyFile := q.Get("key")
		key, err = ioutil.ReadFile(keyFile)
		if err != nil || len(key) == 0 {
			return nil, ErrInvalidKeyFile
		}

		params.TLSMode = 2
		params.Certificate, err = tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, ErrInvalidCert
		}
	case "anon", "":
		params.TLSMode = 1
	case "none":
		params.TLSMode = 0

	default:
		return nil, ErrInvalidTLSMode
	}

	if t := q.Get("timeout"); t != "" {
		d := parseDuration(t)
		if d == 0 {
			return nil, ErrInvalidDuration
		}
		params.ConnectTimeout = d
		params.ReadTimeout = d
	}

	if t := q.Get("read-timeout"); t != "" {
		d := parseDuration(t)
		if d == 0 {
			return nil, ErrInvalidDuration
		}
		params.ReadTimeout = d
	}

	if t := q.Get("retries"); t != "" {
		r, err := strconv.Atoi(t)
		if err != nil {
			return nil, ErrInvalidRetries
		}
		params.RetryCount = r
	}

	if s := q.Get("pool"); s != "" {
		size, err := strconv.Atoi(s)
		if err != nil {
			return nil, ErrInvalidRetries
		}
		params.PoolSize = size
	}

	if t := q.Get("leader"); t != "" {
		switch t {
		case "1", "true", "yes":
			params.LeaderOnly = true
		default:
			params.LeaderOnly = false
		}
	}

	return params, nil
}

var reDur = regexp.MustCompile(`^(\d+)\s*(h|m|s|ms|us|µs|ns)$`)

func parseDuration(dur string) (d time.Duration) {
	m := reDur.FindStringSubmatch(dur)
	if len(m) == 3 {
		v, _ := strconv.Atoi(m[1])
		d = time.Duration(v)
		switch m[2] {
		case "µs", "us":
			d *= time.Microsecond
		case "ms":
			d *= time.Millisecond
		case "s":
			d *= time.Second
		case "m":
			d *= time.Minute
		case "h":
			d *= time.Hour
		}
	}
	return
}

var durUnits = []struct {
	d time.Duration
	s string
}{
	{time.Hour, "h"},
	{time.Minute, "m"},
	{time.Second, "s"},
	{time.Millisecond, "ms"},
	{time.Microsecond, "µs"},
	{time.Nanosecond, "ns"},
}

func renderDuration(duration time.Duration) string {
	for _, u := range durUnits {
		if (duration/u.d)*u.d == duration {
			return fmt.Sprintf("%d%s", duration/u.d, u.s)
		}
	}
	return ""
}
