// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package resolver connects configured sources on first use. It sits between
// the invocation handlers and the primitive store so that the store stays a
// plain get/set repository with no fallible I/O on it.
package resolver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/googleapis/mcp-toolbox/internal/sources"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

// SourceInitTimeout bounds a deferred source connection. The attempt is shared
// by every caller waiting on it, so it cannot take any single caller's deadline;
// this ceiling is what stops a partitioned network from pinning it open. It is
// sized for a cold cloud connector path (token fetch, instance metadata lookup,
// TLS handshake) rather than for a healthy connection.
const SourceInitTimeout = 60 * time.Second

// SourceResolver hands a connected source to invocation paths, connecting it
// first when lazy initialization deferred it, and caching what it connects.
//
// A source that has not been reached yet is absent from both the store and the
// cache, so every lookup keeps returning the (Source, bool) shape callers
// already handle. Nothing ever stands in for an unconnected source, which is
// what keeps a half-built value away from the type assertions every tool
// performs on the source it is given.
type SourceResolver struct {
	// store reads sources that eager initialization already connected. The
	// resolver never writes back to it, which is what keeps the primitive store
	// a plain get/set repository.
	store sources.Getter

	mu      sync.RWMutex
	lazy    bool
	configs map[string]sources.SourceConfig
	tracer  trace.Tracer

	// connected holds the sources this resolver initialized. It lives here
	// rather than in the store so that deferring a connection stays entirely
	// within the server layer.
	connected map[string]sources.Source

	// initGroup, not mu, is what serializes connecting. mu is held only across
	// map reads; a mutex held across Initialize would queue unrelated sources
	// behind one slow connect, whereas singleflight keys on the source name.
	initGroup singleflight.Group
}

// New returns a resolver over store. Until SetLazySources is called it serves
// only sources the store already holds, which is the eager startup behavior.
func New(store sources.Getter) *SourceResolver {
	return &SourceResolver{store: store, connected: make(map[string]sources.Source)}
}

// SetLazySources enables deferred initialization against the given configs and
// drops anything connected against the previous ones. Callers that replace the
// store's contents must call this again, since the previous configs no longer
// describe the sources that were just swapped in.
func (r *SourceResolver) SetLazySources(configs map[string]sources.SourceConfig, tracer trace.Tracer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lazy = true
	r.configs = configs
	r.tracer = tracer
	r.connected = make(map[string]sources.Source)
}

// GetSource returns a source only if it is already connected, whether by eager
// startup or by an earlier Resolve. It never blocks and never fails, so listing
// paths can use it and fall back to a tool's static manifest on a miss.
func (r *SourceResolver) GetSource(sourceName string) (sources.Source, bool) {
	r.mu.RLock()
	source, ok := r.connected[sourceName]
	r.mu.RUnlock()
	if ok {
		return source, true
	}
	return r.store.GetSource(sourceName)
}

// Lazy reports whether sources are connected on first use.
func (r *SourceResolver) Lazy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lazy
}

// Resolve returns the named source, connecting it first if it has not been
// reached yet. Unlike a store read it blocks on I/O and can fail, so it belongs
// on invocation paths only. It returns as soon as ctx is done, bounding the wait
// by the caller's own deadline while the shared attempt continues under
// SourceInitTimeout.
func (r *SourceResolver) Resolve(ctx context.Context, sourceName string) (sources.Source, error) {
	if source, connected := r.GetSource(sourceName); connected {
		return source, nil
	}

	r.mu.RLock()
	sourceConfig, configured := r.configs[sourceName]
	tracer := r.tracer
	r.mu.RUnlock()

	if !configured {
		return nil, fmt.Errorf("unable to retrieve source %q", sourceName)
	}

	// singleflight collapses the connection attempts that race on a cold
	// source. A failure is deliberately not cached, so a source that was down
	// or misconfigured starts working on a later call without a restart.
	ch := r.initGroup.DoChan(sourceName, func() (any, error) {
		// A caller that queued behind a winner which already finished would
		// otherwise start a second connect, since singleflight only shares an
		// attempt that is still in flight.
		if source, connected := r.GetSource(sourceName); connected {
			return source, nil
		}

		// The attempt is shared by every caller waiting on it, so it must not
		// inherit the cancellation of whichever caller happened to start it.
		// WithoutCancel keeps the trace context so the span still parents.
		initCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), SourceInitTimeout)
		defer cancel()

		childCtx, span := tracer.Start(
			initCtx,
			"toolbox/server/source/init",
			trace.WithAttributes(attribute.String("source_type", sourceConfig.SourceConfigType())),
			trace.WithAttributes(attribute.String("source_name", sourceName)),
		)
		defer span.End()

		source, err := sourceConfig.Initialize(childCtx, tracer)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("unable to initialize source %q: %w", sourceName, err)
		}

		r.mu.Lock()
		r.connected[sourceName] = source
		r.mu.Unlock()
		return source, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		source, _ := res.Val.(sources.Source)
		return source, nil
	case <-ctx.Done():
		// Only this caller gives up; the shared attempt runs on for the others
		// and caches the source if it succeeds.
		return nil, fmt.Errorf("unable to initialize source %q: %w", sourceName, ctx.Err())
	}
}
