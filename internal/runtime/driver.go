package runtime

// Run is the driver loop (design.md §4): pull one row from top, write it
// to sink, repeat until top is exhausted. Exactly one row is in flight at
// a time — nothing here buffers the stream.
func Run(top Stream, sink Sink) error {
	for {
		row, ok := top.Next()
		if !ok {
			break
		}
		if err := sink.Write(row); err != nil {
			return err
		}
	}
	return sink.Close()
}
