// Package api defines the transport abstraction over LinkedIn's APIs and
// houses the official REST adapter plus a pluggable fallback for unofficial
// transports. Commands interact with LinkedIn exclusively through the
// Transport interface so the wire details stay here.
package api
