// Package scrape fetches the upstream card gallery from
// playriftbound.com and transforms it into the Riftcodex JSON shape.
//
// The package has four layers, each at a single seam:
//
//   - client.go    — HTTP client with timeout, retry, and a custom
//                    User-Agent. The system boundary.
//   - parse.go     — extracts the __NEXT_DATA__ JSON blob from the
//                    gallery's initial HTML.
//   - transform.go — converts a single upstream Card into a
//                    domain.Card. The richest seam; TDD here.
//   - sync.go      — orchestrates fetch → parse → transform → write.
//                    Calls into the store package.
//
// The transformer's contract is fully covered by testdata/gallery/ and
// transform_test.go.
package scrape
