package grazer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tj/assert"
)

func Test_queue_enqueue(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		q := newQueue()
		q.enqueue([]string{"/contact"}, []string{"/about", "/home"})

		assertPop(t, q, "/contact")
		assertPop(t, q, "/about")
		assertPop(t, q, "/home")
	})

	t.Run("multiple will combine priority, keep unique", func(t *testing.T) {
		q := newQueue()
		q.enqueue([]string{"/contact"}, []string{"/about", "/home"})
		q.enqueue([]string{"/about"}, []string{"/contact", "/home"})

		assertPop(t, q, "/contact")
		assertPop(t, q, "/about")
		assertPop(t, q, "/home")
		assert.Nil(t, q.pop())
	})

	t.Run("intermittent 1", func(t *testing.T) {
		q := newQueue()
		q.enqueue([]string{"/contact", "/support"}, []string{"/about", "/home", "/imprint"})

		assertPop(t, q, "/contact")

		q.enqueue([]string{"/imprint", "/contact"}, []string{"/about", "/home", "/support"})

		assertPop(t, q, "/support")
		assertPop(t, q, "/contact")
		assertPop(t, q, "/imprint")
		assertPop(t, q, "/about")
		assertPop(t, q, "/home")
		assert.Nil(t, q.pop())
	})

	t.Run("intermittent 2", func(t *testing.T) {
		q := newQueue()
		q.enqueue([]string{"/contact", "/support"}, []string{"/about", "/home", "/imprint"})

		assertPop(t, q, "/contact")
		assertPop(t, q, "/support")
		assertPop(t, q, "/about")

		q.enqueue([]string{"/imprint", "/contact"}, []string{"/about", "/home", "/support"})

		assertPop(t, q, "/contact")
		assertPop(t, q, "/imprint")
		assertPop(t, q, "/about")
		assertPop(t, q, "/home")
		assertPop(t, q, "/support")
		assert.Nil(t, q.pop())
	})
}

func assertPop(t *testing.T, q *queue, expectedRoutePath string) {
	t.Helper()

	routePath := q.pop()
	require.NotNil(t, routePath)
	assert.Equal(t, expectedRoutePath, *routePath)
}
