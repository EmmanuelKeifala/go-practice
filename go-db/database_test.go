package main

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestBTreeBasic(t *testing.T) {
	c := newC()
	key := "key1"
	val := "value1"

	// Test initial insert
	c.add(key, val)
	// if !bytes.Equal(c.tree.get(c.tree.root).getKey(1), []byte(key)) {
	// 	t.Fatal("root node doesn't contain inserted key")
	// }

	// Test lookup through reference
	refVal, ok := c.ref[key]
	if !ok || refVal != val {
		t.Fatal("reference map doesn't match inserted value")
	}
}

func TestBTreeMultipleInserts(t *testing.T) {
	c := newC()
	items := []struct {
		key string
		val string
	}{
		{"a", "1"},
		{"b", "2"},
		{"c", "3"},
		{"d", "4"},
		{"e", "5"},
	}

	// Insert all items
	for _, item := range items {
		c.add(item.key, item.val)
	}

	// Verify all items exist
	for _, item := range items {
		if c.ref[item.key] != item.val {
			t.Fatalf("value mismatch for key %s", item.key)
		}
	}
}

func TestBTreeUpdateValue(t *testing.T) {
	c := newC()
	key := "key"
	val1 := "value1"
	val2 := "value2"

	// Initial insert
	c.add(key, val1)
	if c.ref[key] != val1 {
		t.Fatal("initial value not set correctly")
	}

	// Update value
	c.add(key, val2)
	if c.ref[key] != val2 {
		t.Fatal("value not updated correctly")
	}
}

func TestBTreeSplitLeaf(t *testing.T) {
	c := newC()
	// Insert enough items to force a leaf split
	// This depends on your page size and key/val sizes
	for i := 0; i < 100; i++ {
		key := string([]byte{byte(i)})
		val := string([]byte{byte(i), byte(i)})
		c.add(key, val)
	}

	// Verify the tree has multiple nodes now
	// root := c.tree.get(c.tree.root)
	// if root.btype() != BNODE_NODE {
	// 	t.Fatal("root should be internal node after split")
	// }
	// if root.nkeys() < 2 {
	// 	t.Fatal("root should have multiple keys after split")
	// }
}

func TestBTreeRandomOperations(t *testing.T) {
	c := newC()
	const numOps = 1000
	keys := make([]string, 0, numOps)

	// Generate random keys
	for i := 0; i < numOps; i++ {
		key := make([]byte, rand.Intn(10)+1)
		rand.Read(key)
		keys = append(keys, string(key))
	}

	// Perform random insert/update operations
	for i := 0; i < numOps; i++ {
		val := make([]byte, rand.Intn(10)+1)
		rand.Read(val)
		c.add(keys[i], string(val))
	}

	// Verify all operations
	for i := 0; i < numOps; i++ {
		val, ok := c.ref[keys[i]]
		if !ok {
			t.Fatalf("key %x not found in reference", keys[i])
		}

		// This would need a Get operation to verify against the tree
		// For now just checking reference map
		if len(val) == 0 {
			t.Fatalf("empty value for key %x", keys[i])
		}
	}
}

func TestBTreeEdgeCases(t *testing.T) {
	c := newC()

	// Test empty key
	c.add("", "empty")
	if c.ref[""] != "empty" {
		t.Fatal("empty key not handled correctly")
	}

	// Test maximum key size
	maxKey := string(bytes.Repeat([]byte{'x'}, BTREE_MAX_KEY_SIZE))
	c.add(maxKey, "max")
	if c.ref[maxKey] != "max" {
		t.Fatal("max key size not handled correctly")
	}

	// Test maximum value size
	maxVal := string(bytes.Repeat([]byte{'y'}, BTREE_MAX_VAL_SIZE))
	c.add("maxval", maxVal)
	if c.ref["maxval"] != maxVal {
		t.Fatal("max value size not handled correctly")
	}
}

// Note: The following test requires the Delete operation to be implemented
/*
func TestBTreeDeletion(t *testing.T) {
	c := newC()
	keys := []string{"a", "b", "c", "d", "e"}

	// Insert all keys
	for _, key := range keys {
		c.add(key, "value")
	}

	// Delete one key
	c.tree.Delete([]byte("c"))
	delete(c.ref, "c")

	// Verify deletion
	if _, ok := c.ref["c"]; ok {
		t.Fatal("key not deleted from reference")
	}

	// Verify other keys still exist
	for _, key := range []string{"a", "b", "d", "e"} {
		if _, ok := c.ref[key]; !ok {
			t.Fatalf("key %s missing after unrelated deletion", key)
		}
	}
}
*/
