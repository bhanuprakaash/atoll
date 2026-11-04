package memory

import (
	"context"
	"encoding/binary"
	"math"
)

// Embedder turns text into a vector. A nil vector with a nil error means none.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type noneEmbedder struct{}

func (noneEmbedder) Embed(context.Context, string) ([]float32, error) { return nil, nil }

func embedText(body, why string) string {
	if why == "" {
		return body
	}
	return body + "\n" + why
}

func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
