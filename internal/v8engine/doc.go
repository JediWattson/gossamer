// Package v8engine adapts an unmodified upstream V8 build to Gossamer's
// engine-neutral browser socket.
//
// The implementation is enabled with the v8 build tag. JavaScript values stay
// entirely inside V8's moving heap; Go receives only opaque numeric identity
// once DOM wrappers are introduced.
package v8engine
