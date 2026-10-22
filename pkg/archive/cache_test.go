package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regressão do patch medgrupo.7: ninguém chama Release() (Manager.Release não
// tem chamador; events/check.go só faz Clone), então todo archive fica com
// refCount >= 1 para sempre. A limpeza por TTL pulava essas entradas e o cache
// nunca apagava nada — disco e slab do kernel (dentries/inodes das árvores
// extraídas) cresciam sem teto até o OOM do pod no hub (22/10/2026).
func TestCleanupStaleArchives_RemovesStaleEntryNeverReleased(t *testing.T) {
	base := t.TempDir()
	c := NewCache(Config{BaseDir: base, TTL: time.Hour})
	defer c.Shutdown()

	extracted := filepath.Join(base, "sha-stale", "owner-repo-sha-stale")
	require.NoError(t, os.MkdirAll(extracted, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extracted, "values.yaml"), []byte("a: 1\n"), 0o644))

	c.lock.Lock()
	c.entries["sha-stale"] = &CacheEntry{
		extractedPath: extracted,
		lastUsed:      time.Now().Add(-2 * time.Hour),
		refCount:      1, // adquirido e nunca liberado, como em produção
	}
	c.lock.Unlock()

	c.cleanupStaleArchives()

	_, err := os.Stat(filepath.Dir(extracted))
	assert.True(t, os.IsNotExist(err), "diretório do archive vencido deveria ter sido removido do disco")
	c.lock.RLock()
	defer c.lock.RUnlock()
	assert.Empty(t, c.entries, "entrada vencida deveria sair do mapa")
}

func TestCleanupStaleArchives_KeepsFreshEntry(t *testing.T) {
	base := t.TempDir()
	c := NewCache(Config{BaseDir: base, TTL: time.Hour})
	defer c.Shutdown()

	extracted := filepath.Join(base, "sha-fresh", "owner-repo-sha-fresh")
	require.NoError(t, os.MkdirAll(extracted, 0o755))

	c.lock.Lock()
	c.entries["sha-fresh"] = &CacheEntry{
		extractedPath: extracted,
		lastUsed:      time.Now().Add(-10 * time.Minute),
		refCount:      1,
	}
	c.lock.Unlock()

	c.cleanupStaleArchives()

	_, err := os.Stat(extracted)
	assert.NoError(t, err, "archive dentro do TTL deve continuar no disco")
	c.lock.RLock()
	defer c.lock.RUnlock()
	assert.Len(t, c.entries, 1)
}
