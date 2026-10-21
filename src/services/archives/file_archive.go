package archives

import "io"

const MaxArchiveFiles = 1000
const MaxArchiveBytes int64 = 10 * 1024 * 1024 * 1024

type ArchiveSelection struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
}

type ArchiveProgressWriter struct {
	Writer io.Writer   `json:"-"`
	Add    func(int64) `json:"-"`
	Failed bool        `json:"-"`
}

func (p *ArchiveProgressWriter) Write(value []byte) (int, error) {
	n, err := p.Writer.Write(value)
	if n > 0 {
		p.Add(int64(n))
	}
	if err != nil {
		p.Failed = true
	}
	return n, err
}
