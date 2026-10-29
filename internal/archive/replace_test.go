package archive

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store/migrations"
)

// TestReplacingOneKindLeavesTheOthers: an instance changed since its
// backup, its assets replaced: they are the backup's again, files and all,
// and the catalog, labels and people are as changed.
func TestReplacingOneKindLeavesTheOthers(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, st, blobs)
	b := backup(t, st, blobs, Options{})
	want := contents(t, b)

	// Changed since: an asset added, one renamed, a photo's file gone, a
	// category renamed, and someone new.
	cat, err := st.CreateCategory("Added since", nil, "#000000", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAsset("Saw", []string{cat.ID}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("UPDATE assets SET name = ? WHERE name = ?", "Drill (renamed)", "Drill"); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Delete(ctx, "photos/"+s.photo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("UPDATE categories SET name = ? WHERE name = ?", "Tools (renamed)", "Tools"); err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser("bob@example.com", "bobs own password", true)
	if err != nil {
		t.Fatal(err)
	}
	before := contents(t, backup(t, st, blobs, Options{}))

	sum, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Kinds: []Kind{KindAssets}, Replace: true, Keep: s.user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Removed["assets"] != 1 || sum.Tables["assets"] != 1 {
		t.Errorf("removed %v, restored %v", sum.Removed, sum.Tables)
	}
	after := contents(t, backup(t, st, blobs, Options{}))
	for k, v := range after {
		assets := false
		for _, table := range Tables {
			if table.Kind == KindAssets && (k == DataDir+table.Name || k == AuthDir+table.Name) {
				assets = true
			}
		}
		assets = assets || strings.HasPrefix(k, FilesDir+"photos/")
		if assets && v != want[k] {
			t.Errorf("%s is not the backup's:\nwant %.300s\ngot  %.300s", k, want[k], v)
		}
		if !assets && v != before[k] {
			t.Errorf("%s changed, though not replaced", k)
		}
	}
	if count(t, st, "users") != 2 {
		t.Errorf("bob (%s) went", bob.ID)
	}
}

// TestReplacingPeopleKeepsTheInstancesOwn: the backup's people are put
// back, and everyone else, with their passwords and roles, stays.
func TestReplacingPeopleKeepsTheInstancesOwn(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, st, blobs)
	b := backup(t, st, blobs, Options{})

	if _, err := st.Exec("UPDATE users SET name = ? WHERE id = ?", "Ada Changed", s.user.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPassword(s.user.ID, "a newer long password", ""); err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser("bob@example.com", "bobs own password", true)
	if err != nil {
		t.Fatal(err)
	}
	var admin string
	if err := st.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", bob.ID, admin); err != nil {
		t.Fatal(err)
	}

	sum, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Kinds: []Kind{KindPeople}, Replace: true, Keep: s.user.ID})
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := st.QueryRow("SELECT name FROM users WHERE id = ?", s.user.ID).Scan(&name); err != nil || name != awkwardText[5] {
		t.Errorf("Ada's name is %q (%v), not the backup's", name, err)
	}
	if !signsIn(t, st, "ada@example.com", "a newer long password") {
		t.Error("Ada's password was changed by a replace without authentication")
	}
	if !signsIn(t, st, "bob@example.com", "bobs own password") {
		t.Error("Bob, not in the backup, cannot sign in")
	}
	var n int
	if err := st.QueryRow("SELECT COUNT(*) FROM user_roles WHERE user_id = ?", bob.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("Bob has %d roles", n)
	}
	if len(sum.Removed) != 0 {
		t.Errorf("removed %v", sum.Removed)
	}
}

func TestAReplaceThatWouldLockPeopleOutIsRefused(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, st, blobs)
	// A backup in which Ada was turned off.
	if _, err := st.Exec("UPDATE users SET is_active = ? WHERE id = ?", false, s.user.ID); err != nil {
		t.Fatal(err)
	}
	b := backup(t, st, blobs, Options{})
	if _, err := st.Exec("UPDATE users SET is_active = ? WHERE id = ?", true, s.user.ID); err != nil {
		t.Fatal(err)
	}
	before := contents(t, backup(t, st, blobs, Options{}))
	_, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Kinds: []Kind{KindPeople}, Replace: true, Keep: s.user.ID})
	if !errors.Is(err, ErrLockout) {
		t.Fatalf("got %v, want ErrLockout", err)
	}
	sameContents(t, before, contents(t, backup(t, st, blobs, Options{})))
}

func TestReplaceTakesOnlyABackupOfThisSchema(t *testing.T) {
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	richSeed(t, st, blobs)
	older := pack(t, reseal(t, unpack(t, backup(t, st, blobs, Options{Kinds: []Kind{KindLabels}})), func(h *Header, _ *Manifest) { h.SchemaVersion = migrations.Latest() - 1 }))
	if _, err := Restore(context.Background(), st, blobs, opener(older), RestoreOptions{Replace: true}); !errors.Is(err, ErrSchema) {
		t.Errorf("replaced with an older schema's backup: %v", err)
	}
}

// TestReplacingWithAnotherInstancesBackupIsRefused: its admin role has
// another ID, and the same name, so nothing is changed.
func TestReplacingWithAnotherInstancesBackupIsRefused(t *testing.T) {
	ctx := context.Background()
	other := migratedStore(t, envDialect())
	otherBlobs := filestore.NewLocal(t.TempDir())
	richSeed(t, other, otherBlobs)
	b := backup(t, other, otherBlobs, Options{})

	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := seed(t, st, blobs)
	var admin string
	if err := st.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", s.user.ID, admin); err != nil {
		t.Fatal(err)
	}
	before := contents(t, backup(t, st, blobs, Options{}))
	if _, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Replace: true, Keep: s.user.ID}); err == nil {
		t.Fatal("replaced with another instance's backup")
	} else {
		t.Logf("refused: %v", err)
	}
	sameContents(t, before, contents(t, backup(t, st, blobs, Options{})))
}

// TestReplacingEverythingMakesTheBackupAgain: every kind replaced, the
// instance is the backup again, but for people it never had, who stay.
func TestReplacingEverythingMakesTheBackupAgain(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, st, blobs)
	b := backup(t, st, blobs, Options{})
	want := contents(t, b)

	cat, _ := st.CreateCategory("Added since", nil, "#000000", false, false)
	st.CreateAsset("Saw", []string{cat.ID}, nil, nil)
	st.Exec("DELETE FROM templates")
	if _, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Replace: true, Keep: s.user.ID}); err != nil {
		t.Fatal(err)
	}
	sameContents(t, want, contents(t, backup(t, st, blobs, Options{})))
}

// TestRestoringWithAWipe puts an instance back as it was, though it was
// changed since, its schema too.
func TestRestoringWithAWipe(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t, envDialect())
	blobs := filestore.NewLocal(t.TempDir())
	s := richSeed(t, st, blobs)
	b := backup(t, st, blobs, Options{Secrets: true})
	want := contents(t, b)

	// Changed since, as an update that failed would: a user added, a
	// table made, a photo's file removed.
	if _, err := st.CreateUser("upgraded@example.com", "a long marker password", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Exec("CREATE TABLE from_a_newer_bam (id VARCHAR(36) PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := blobs.Delete(ctx, "photos/"+s.photo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Secrets: true, Wipe: true}); err != nil {
		t.Fatal(err)
	}
	sameContents(t, want, contents(t, backup(t, st, blobs, Options{Secrets: true})))
	exist, err := existingTables(ctx, st)
	if err != nil || exist["from_a_newer_bam"] {
		t.Errorf("the newer table is still there (%v)", err)
	}
	if _, err := Restore(ctx, st, blobs, opener(b), RestoreOptions{Wipe: true, Replace: true}); err == nil {
		t.Error("wiped and replaced at once")
	}
}
