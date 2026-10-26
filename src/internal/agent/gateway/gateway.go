package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"tangentry/internal/agent"
)

const ProtocolVersion = "1"

const maxConnectionFileBytes = 16 << 10

type Connection struct {
	Endpoint  string   `json:"endpoint"`
	Token     string   `json:"token"`
	Version   string   `json:"version"`
	ExpiresAt int64    `json:"expiresAt"`
	Scopes    []string `json:"scopes"`
}

type Gateway struct {
	server     *http.Server
	listener   net.Listener
	handler    *handler
	token      string
	file       string
	connection Connection
}

func Start(service *agent.Service, dataDir string) (*Gateway, error) {
	return StartWithScopes(service, dataDir, []string{agent.ScopeContentRead, agent.ScopeAgentWrite})
}

func StartWithScopes(service *agent.Service, dataDir string, scopes []string) (*Gateway, error) {
	if service == nil {
		return nil, errors.New("agent service unavailable")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		listener.Close()
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	path := filepath.Join(dataDir, "agent", "connection.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		listener.Close()
		return nil, err
	}
	connection := Connection{Endpoint: "http://" + listener.Addr().String(), Token: token, Version: ProtocolVersion, ExpiresAt: time.Now().Add(24 * time.Hour).Unix(), Scopes: normalizeScopes(scopes)}
	data, err := json.MarshalIndent(connection, "", "  ")
	if err != nil {
		listener.Close()
		return nil, err
	}
	if err := atomicSecretFile(path, data); err != nil {
		listener.Close()
		return nil, err
	}
	handler := newHandler(service, token, connection.Scopes, listener.Addr().String(), connection.ExpiresAt)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	gateway := &Gateway{server: server, listener: listener, handler: handler, token: token, file: path, connection: connection}
	go func() { _ = server.Serve(listener) }()
	return gateway, nil
}

func normalizeScopes(scopes []string) []string {
	allowed := map[string]bool{agent.ScopeContentRead: true, agent.ScopeNetworkFetch: true, agent.ScopeImageRead: true, agent.ScopePrivateNotes: true, agent.ScopeDerived: true, agent.ScopeAgentWrite: true}
	seen := map[string]bool{}
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if allowed[scope] && !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result
}

func atomicSecretFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".connection-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (g *Gateway) Close() error {
	if g == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Revoke before shutdown so requests already accepted by net/http cannot
	// continue using the bearer token during the close window.
	if g.handler != nil {
		g.handler.revoke()
	}
	err := g.server.Shutdown(ctx)
	_ = os.Remove(g.file)
	return err
}

func ReadConnection(path string) (Connection, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Connection{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 || info.Size() < 0 || info.Size() > maxConnectionFileBytes {
		return Connection{}, errors.New("agent connection file permissions must be 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Connection{}, err
	}
	var connection Connection
	if err := json.Unmarshal(data, &connection); err != nil {
		return Connection{}, err
	}
	parsed, parseErr := url.Parse(connection.Endpoint)
	if parseErr != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || parsed.Port() == "" {
		return Connection{}, errors.New("agent connection file is invalid or expired")
	}
	port, portErr := strconv.Atoi(parsed.Port())
	if portErr != nil || port < 1 || port > 65535 {
		return Connection{}, errors.New("agent connection file is invalid or expired")
	}
	if len(connection.Token) != 64 {
		return Connection{}, errors.New("agent connection file is invalid or expired")
	}
	if _, tokenErr := hex.DecodeString(connection.Token); tokenErr != nil || connection.Version != ProtocolVersion || time.Now().Unix() >= connection.ExpiresAt {
		return Connection{}, errors.New("agent connection file is invalid or expired")
	}
	connection.Scopes = normalizeScopes(connection.Scopes)
	return connection, nil
}

func (g *Gateway) String() string {
	if g == nil {
		return "agent gateway disabled"
	}
	return fmt.Sprintf("agent gateway listening at %s", g.listener.Addr())
}

// Connection returns the current in-memory connection descriptor. Callers
// must treat the token as a secret and only expose it through an authenticated
// settings flow.
func (g *Gateway) Connection() Connection {
	if g == nil {
		return Connection{}
	}
	return g.connection
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, max int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Method != http.MethodPost || err != nil || mediaType != "application/json" {
		return errors.New("POST application/json required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func constantTokenMatch(expected, supplied string) bool {
	return len(expected) == len(supplied) && subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}
