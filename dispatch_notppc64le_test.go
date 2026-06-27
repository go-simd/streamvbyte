//go:build !ppc64le

package streamvbyte

import "testing"

// forceEncodeKernel is a no-op off ppc64le: on the other arches the SIMD encode
// path is engaged by the normal feature dispatch, so the tight-buffer test
// already covers encodeSafeGroups without forcing anything.
func forceEncodeKernel(t *testing.T) { t.Helper() }
