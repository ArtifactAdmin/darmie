package ws

import (
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"

	"darmie/internal/domain"
	"darmie/internal/protocol"
)

// Presence errors surfaced to clients verbatim.
var (
	errRoomNotFound  = errors.New("room not found")
	errRoomFull      = errors.New("room is full")
	errRoomExists    = errors.New("a room with that name already exists")
	errTooManyRooms  = errors.New("maximum number of rooms reached")
	errRoomNameRange = errors.New("room name must be 1–50 characters")
)

// room is the live (in-memory) membership of a chat room. Persistent history
// lives in the database, keyed by room name; presence is purely runtime state.
type room struct {
	id      string
	name    string
	mu      sync.RWMutex
	clients map[string]*Client // userID → Client
}

func newRoom(name string) *room {
	return &room{id: uuid.NewString(), name: name, clients: make(map[string]*Client)}
}

// snapshot returns a copy of the current member list under the lock.
func (r *room) snapshot() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// broadcast sends data to every member except exclude.
func (r *room) broadcast(data []byte, exclude string) {
	for _, c := range r.snapshot() {
		if c.userID != exclude {
			c.trySend(data)
		}
	}
}

// Registry is the in-memory presence model: connected clients and live rooms.
// It is the single owner of that mutable state, isolating concurrency control
// to one place.
type Registry struct {
	mu          sync.RWMutex
	clients     map[string]*Client // userID → authenticated Client
	rooms       map[string]*room   // roomID → room
	maxRooms    int
	maxRoomSize int
}

// NewRegistry returns an empty Registry with the given capacity caps.
func NewRegistry(maxRooms, maxRoomSize int) *Registry {
	return &Registry{
		clients:     make(map[string]*Client),
		rooms:       make(map[string]*room),
		maxRooms:    maxRooms,
		maxRoomSize: maxRoomSize,
	}
}

// AddClient registers c as the live connection for its user, evicting and
// returning any prior connection for the same user (nil if none). The swap is
// atomic so concurrent logins cannot interleave evict and register.
func (g *Registry) AddClient(c *Client) (evicted *Client) {
	g.mu.Lock()
	old := g.clients[c.userID]
	g.clients[c.userID] = c
	g.mu.Unlock()
	if old != nil && old != c {
		return old
	}
	return nil
}

// RemoveClient unregisters c, but only if it is still the live connection.
func (g *Registry) RemoveClient(c *Client) {
	g.mu.Lock()
	if g.clients[c.userID] == c {
		delete(g.clients, c.userID)
	}
	g.mu.Unlock()
}

// Client returns the live connection for a user, if any.
func (g *Registry) Client(userID string) (*Client, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.clients[userID]
	return c, ok
}

// CreateRoom creates a uniquely-named room, enforcing the room-count cap.
func (g *Registry) CreateRoom(name string) (*room, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > domain.MaxRoomNameLen {
		return nil, errRoomNameRange
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.rooms) >= g.maxRooms {
		return nil, errTooManyRooms
	}
	for _, r := range g.rooms {
		if strings.EqualFold(r.name, name) {
			return nil, errRoomExists
		}
	}
	r := newRoom(name)
	g.rooms[r.id] = r
	return r, nil
}

// Room returns a room by ID.
func (g *Registry) Room(id string) (*room, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	r, ok := g.rooms[id]
	return r, ok
}

// ListRooms returns a snapshot of every room as wire info.
func (g *Registry) ListRooms() []protocol.RoomInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	infos := make([]protocol.RoomInfo, 0, len(g.rooms))
	for _, r := range g.rooms {
		r.mu.RLock()
		infos = append(infos, protocol.RoomInfo{ID: r.id, Name: r.name, UserCount: len(r.clients)})
		r.mu.RUnlock()
	}
	return infos
}

// AuthedClientsExcept returns every authenticated client except the given user.
func (g *Registry) AuthedClientsExcept(exclude string) []*Client {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Client, 0, len(g.clients))
	for _, c := range g.clients {
		if c.userID != exclude {
			out = append(out, c)
		}
	}
	return out
}

// Join adds c to a room (after enforcing capacity) and returns the room plus a
// snapshot of the members already present. The single-room invariant is the
// caller's responsibility: call LeaveAll first.
func (g *Registry) Join(c *Client, roomID string) (r *room, existing []*Client, err error) {
	g.mu.RLock()
	r, ok := g.rooms[roomID]
	g.mu.RUnlock()
	if !ok {
		return nil, nil, errRoomNotFound
	}

	r.mu.Lock()
	if len(r.clients) >= g.maxRoomSize {
		r.mu.Unlock()
		return nil, nil, errRoomFull
	}
	existing = make([]*Client, 0, len(r.clients))
	for _, ec := range r.clients {
		existing = append(existing, ec)
	}
	r.clients[c.userID] = c
	r.mu.Unlock()

	c.addRoom(r.id)
	return r, existing, nil
}

// Leave removes c from r. It returns the remaining members and whether c was a
// member that was actually removed.
func (g *Registry) Leave(c *Client, r *room) (remaining []*Client, removed bool) {
	r.mu.Lock()
	if _, member := r.clients[c.userID]; !member {
		r.mu.Unlock()
		return nil, false
	}
	delete(r.clients, c.userID)
	remaining = make([]*Client, 0, len(r.clients))
	for _, rc := range r.clients {
		remaining = append(remaining, rc)
	}
	r.mu.Unlock()

	c.removeRoom(r.id)
	return remaining, true
}

// RoomsOf returns the rooms c currently belongs to.
func (g *Registry) RoomsOf(c *Client) []*room {
	ids := c.roomIDs()
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*room, 0, len(ids))
	for _, id := range ids {
		if r, ok := g.rooms[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// GhostRooms scans every room for a stale membership of c that is not reflected
// in c's own room set. This guards the narrow window in Join between adding c to
// a room and updating c's membership.
func (g *Registry) GhostRooms(c *Client) []*room {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var ghosts []*room
	for _, r := range g.rooms {
		r.mu.RLock()
		if _, ok := r.clients[c.userID]; ok {
			ghosts = append(ghosts, r)
		}
		r.mu.RUnlock()
	}
	return ghosts
}
