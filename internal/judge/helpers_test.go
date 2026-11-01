package judge

import "focuslock/internal/config"

const goTask = "Learn Go channels and select: buffered vs unbuffered, closing channels, worker pools"

const goDetail = "Goal: learn Go channels and select.\n" +
	"Topics: buffered and unbuffered channels, closing channels, select, worker pools, goroutines.\n" +
	"Resources: go.dev docs, Go by Example, Go concurrency talks and tutorials.\n" +
	"Not part of it: system design, other languages, general backend roadmaps."

func testConfig() config.Config {
	c := config.Default()
	c.Normalize()
	c.Judge.SettleMs = 0
	return c
}
