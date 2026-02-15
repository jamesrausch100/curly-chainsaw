package ats

import (
	"context"

	"github.com/jamesrausch100/curly-chainsaw/pkg/models"
)

// Client is the interface every ATS platform must implement.
type Client interface {
	// Platform returns which ATS this client handles.
	Platform() models.Platform

	// SearchJobs discovers jobs matching the given query.
	SearchJobs(ctx context.Context, query models.SearchQuery) ([]models.Job, error)

	// GetJob fetches full details for a single job.
	GetJob(ctx context.Context, externalID string) (*models.Job, error)

	// Apply submits an application for a job.
	Apply(ctx context.Context, job models.Job, profile models.Profile, responses map[string]string) (*models.Application, error)

	// ApplicationStatus checks the status of a submitted application.
	ApplicationStatus(ctx context.Context, applicationID string) (models.ApplicationStatus, error)
}

// Registry holds all registered ATS clients.
type Registry struct {
	clients map[models.Platform]Client
}

// NewRegistry creates a new empty registry.
func NewRegistry() *Registry {
	return &Registry{clients: make(map[models.Platform]Client)}
}

// Register adds a client to the registry.
func (r *Registry) Register(c Client) {
	r.clients[c.Platform()] = c
}

// Get returns the client for a given platform.
func (r *Registry) Get(p models.Platform) (Client, bool) {
	c, ok := r.clients[p]
	return c, ok
}

// All returns all registered clients.
func (r *Registry) All() []Client {
	out := make([]Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// Platforms returns all registered platform names.
func (r *Registry) Platforms() []models.Platform {
	out := make([]models.Platform, 0, len(r.clients))
	for p := range r.clients {
		out = append(out, p)
	}
	return out
}
