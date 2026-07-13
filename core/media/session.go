package media

// PTTSession is the facade platform integrations use: one per local user.
// It's a thin wrapper over MeshManager (which does the actual peer
// connection/signaling/talk-loop work) — kept separate so callers have a
// stable, small entry point even as MeshManager's internals evolve (e.g.
// once the relay fallback in RelayDialer is implemented).
type PTTSession struct {
	*MeshManager
}

// NewPTTSession builds a new session for the local device selfID.
func NewPTTSession(selfID string, source AudioSource, sink AudioSink) (*PTTSession, error) {
	mm, err := NewMeshManager(selfID, source, sink)
	if err != nil {
		return nil, err
	}
	return &PTTSession{MeshManager: mm}, nil
}
