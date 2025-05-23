package main

import (
	"bytes"
	"encoding/binary"
	"unsafe"
)

const HEADER = 4
const BTREE_PAGE_SIZE = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000

type BNode []byte

func assert(condition bool) {
	if !condition {
		panic("assertion failed")
	}
}

func init() {
	node1max := HEADER + 8 + 2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE

	assert(node1max <= BTREE_PAGE_SIZE) // maximum KV
}

type BTree struct {
	// pointer (a nonzero page number)
	root uint64

	// callbacks for managing on-disk pages
	get func(uint64) []byte // dereference a pointer
	new func([]byte) uint64 // allocate a new page
	del func(uint64)        // deallocate a page
}

// HEADER
const (
	BNODE_NODE = 1 // internal nodes without values
	BNODE_LEAF = 2 // leaf nodes with values
)

func (node BNode) btype() uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}

func (node BNode) nkeys() uint16 {
	return binary.LittleEndian.Uint16(node[2:4])
}

func (node BNode) setHeader(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	binary.LittleEndian.PutUint16(node[2:4], nkeys)
}

// Child pointers
func (node BNode) getPtr(idx uint16) uint64 {
	assert(idx < node.nkeys())
	// 8 bytes for the header
	pos := HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[pos : pos+8])
}
func (node BNode) setPtr(idx uint16, val uint64) {
	assert(idx < node.nkeys())
	pos := HEADER + 8*idx // Calculate position (header + 8 bytes per pointer)
	binary.LittleEndian.PutUint64(node[pos:pos+8], val)
}

// offset list
func offsetPos(node BNode, idx uint16) uint16 {
	assert(1 <= idx && idx <= node.nkeys())
	return HEADER + 8*node.nkeys() + 2*(idx-1)
}
func (node BNode) getOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0
	}
	return binary.LittleEndian.Uint16(node[offsetPos(node, idx):])
}
func (node BNode) setOffset(idx uint16, offset uint16) {
	assert(1 <= idx && idx <= node.nkeys())
	pos := offsetPos(node, idx)
	binary.LittleEndian.PutUint16(node[pos:], offset)
}

// Key Values
func (node BNode) kvPos(idx uint16) uint16 {
	assert(idx <= node.nkeys())

	return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}

func (node BNode) getKey(idx uint16) []byte {
	assert(idx < node.nkeys())
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])

	return node[pos+4:][:klen]
}

func (node BNode) getVal(idx uint16) []byte {
	assert(idx < node.nkeys())
	pos := node.kvPos(idx)

	// First 2 bytes are key length, next 2 bytes are value length
	klen := binary.LittleEndian.Uint16(node[pos:])
	vlen := binary.LittleEndian.Uint16(node[pos+2:])

	// Value starts after the 4-byte header and key
	valStart := pos + 4 + klen
	return node[valStart : valStart+vlen]
}

// node size in byte
func (node BNode) nbytes() uint16 {
	return node.kvPos(node.nkeys())
}

// returns the first kid node whose range intesects the key (kid[i] <=key)
// TODO: binary search
func nodeLookUpLE(node BNode, key []byte) uint16 {
	nkeys := node.nkeys()
	found := uint16(0)

	// the first key is  copy from the parent node,
	// thus it's always less than or equal to the key
	for i := uint16(1); i < nkeys; i++ {
		cmp := bytes.Compare(node.getKey(i), key)
		if cmp <= 0 {
			found = i
		}
		if cmp >= 0 {
			break
		}
	}
	return found
}

// add a new key to a leaf node
func leafInsert(new BNode, old BNode, idx uint16, key []byte, val []byte) {
	new.setHeader(BNODE_LEAF, old.nkeys()+1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}

// copy a KV into position
func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
	// ptrs
	new.setPtr(idx, ptr)

	// KVs
	pos := new.kvPos(idx)
	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))

	copy(new[pos+4:], key)

	copy(new[pos+4+uint16(len(key)):], val)
	// the offset of the next key
	new.setOffset(idx+1, new.getOffset(idx)+4+uint16(len(key)+len(val)))
}

// copy multiple kvs into the position from the old note
func nodeAppendRange(new BNode, old BNode, dstNew uint16, srcOld uint16, n uint16) {
	assert(srcOld+n <= old.nkeys())
	assert(dstNew+n <= new.nkeys())

	if n == 0 {
		return
	}

	// 1. Copy pointers
	for i := uint16(0); i < n; i++ {
		new.setPtr(dstNew+i, old.getPtr(srcOld+i))
	}

	// 2. Copy offsets
	dstBegin := new.getOffset(dstNew)
	srcBegin := old.getOffset(srcOld)
	for i := uint16(1); i <= n; i++ {
		offset := dstBegin + (old.getOffset(srcOld+i) - srcBegin)
		new.setOffset(dstNew+i, offset)
	}

	// 3. Copy key-value pairs
	begin := old.kvPos(srcOld)
	end := old.kvPos(srcOld + n)
	copy(new[new.kvPos(dstNew):], old[begin:end])
}

// replace a lint with one ore multiple links
func nodeReplaceKidN(tree *BTree, new BNode, old BNode, idx uint16, kids ...BNode) {
	inc := uint16(len(kids))
	new.setHeader(BNODE_NODE, old.nkeys()+inc-1)
	nodeAppendRange(new, old, 0, 0, idx)
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.new(node), node.getKey(0), nil)
	}

	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}

// split a oversized node into 2 so that the 2nd node always fits on a page
func nodeSplit2(left BNode, right BNode, old BNode) {
	// TODO:  will do later
}

// split a node if its too big, the results are 1-3 nodes
func nodeSplit3(old BNode) (uint16, [3]BNode) {
	if old.nbytes() <= BTREE_PAGE_SIZE {
		old = old[:BTREE_PAGE_SIZE]
		return 1, [3]BNode{old} // did not split
	}

	left := BNode(make([]byte, 2*BTREE_PAGE_SIZE)) // might be split later
	right := BNode(make([]byte, BTREE_PAGE_SIZE))
	if left.nbytes() <= BTREE_PAGE_SIZE {
		left = left[:BTREE_PAGE_SIZE]
		return 2, [3]BNode{left, right} // 2 nodes
	}
	leftLeft := BNode(make([]byte, BTREE_PAGE_SIZE))
	middle := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(leftLeft, middle, left)
	assert(leftLeft.nbytes() <= BTREE_PAGE_SIZE)

	return 3, [3]BNode{leftLeft, middle, right} // 3 nodes
	// they are all just temporary data until the nodeReplaceKidN actually allocates them

}

// insert a KV into a node, the result might be split
// the caller is responsivle for deallocationg the input node
//  and splitting and allocationg result nodes

func treeInsert(tree *BTree, node BNode, key []byte, val []byte) BNode {
	//  the result node
	//  it's allowed to be bigger than 1 page and will be split if so
	new := BNode(make([]byte, 2*BTREE_PAGE_SIZE))
	// where to insert the key?
	idx := nodeLookUpLE(node, key)
	switch node.btype() {
	case BNODE_LEAF:
		// leaf, node.getKey(idx) <= key
		if bytes.Equal(key, node.getKey(idx)) {
			// found the key, update it
			// leafUpdate(new, node, idx, key, val)

		} else {
			// insert it fter the position
			leafInsert(new, node, idx+1, key, val)
		}
	case BNODE_NODE:
		//  internal node, insert it to a kid node
		nodeInsert(tree, new, node, idx, key, val)

	default:
		panic("bad node!")
	}

	return new
}

// part of the treeInsert(): KV insertion to an internl node
func nodeInsert(tree *BTree, new BNode, node BNode, idx uint16, key []byte, val []byte) {
	kptr := node.getPtr(idx)
	// recursive insertion to the kid node
	knode := treeInsert(tree, tree.get(kptr), key, val)

	// split the result
	nsplit, split := nodeSplit3(knode)

	// deallocate the kid node
	tree.del(kptr)

	nodeReplaceKidN(tree, new, node, idx, split[:nsplit]...)
}

// HIGH LEVEL INTERFACES

// insert a new key or update an existing key
func (tree *BTree) Insert(key []byte, val []byte) {
	if tree.root == 0 {
		// create the first node
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.setHeader(BNODE_LEAF, 2)

		// a dummy key, this makes the tree cover the whole key space
		// this a lookup can always find a containing node
		nodeAppendKV(root, 0, 0, nil, nil)
		nodeAppendKV(root, 1, 0, key, val)

		tree.root = tree.new(root)

		return

	}

	node := treeInsert(tree, tree.get(tree.root), key, val)
	nsplit, split := nodeSplit3(node)
	tree.del(tree.root)

	if nsplit > 1 {
		// the root was split, add a new level
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.setHeader(BNODE_NODE, nsplit)
		for i, knode := range split[:nsplit] {
			ptr, key := tree.new(knode), knode.getKey(0)
			nodeAppendKV(root, uint16(i), ptr, key, nil)
		}

		tree.root = tree.new(root)
	} else {
		tree.root = tree.new(split[0])
	}
}

// delete a key and returns whether the key was there
func (tree *BTree) Delete(key []byte) bool {
	if tree.root == 0 {
		return false // empty tree
	}

	// Start recursive deletion from the root
	updated := treeDelete(tree, tree.get(tree.root), key)
	if len(updated) == 0 {
		return false // key not found
	}

	tree.del(tree.root) // deallocate old root

	// Handle root updates
	switch updated.btype() {
	case BNODE_NODE:
		if updated.nkeys() == 1 {
			// Root has only one child, make it the new root
			newRoot := tree.get(updated.getPtr(0))
			tree.root = tree.new(newRoot)
			tree.del(updated.getPtr(0)) // deallocate child (it was copied)
			return true
		}
		// Fall through to normal root update
	case BNODE_LEAF:
		if updated.nkeys() == 0 {
			// Tree is now empty
			tree.root = 0
			return true
		}
	}

	// Check if root needs splitting (unlikely but possible)
	if updated.nbytes() <= BTREE_PAGE_SIZE {
		tree.root = tree.new(updated)
	} else {
		// Split the root if it's too large
		nsplit, split := nodeSplit3(updated)
		if nsplit > 1 {
			newRoot := BNode(make([]byte, BTREE_PAGE_SIZE))
			newRoot.setHeader(BNODE_NODE, nsplit)
			for i, knode := range split[:nsplit] {
				ptr := tree.new(knode)
				newRoot.setPtr(uint16(i), ptr)
				newRoot.setKey(uint16(i), knode.getKey(0))
			}
			tree.root = tree.new(newRoot)
		} else {
			tree.root = tree.new(split[0])
		}
	}

	return true
}

// remove a key from a leaf node
func leafDelete(new BNode, old BNode, idx uint16) {
	new.setHeader(BNODE_LEAF, old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+1, old.nkeys()-(idx+1))
}

// merge 2 nodes into 1
func nodeMerge(new BNode, left BNode, right BNode) {
	new.setHeader(left.btype(), left.nkeys()+right.nkeys())
	nodeAppendRange(new, left, 0, 0, left.nkeys())
	nodeAppendRange(new, right, left.nkeys(), 0, right.nkeys())
}

// replace 2 adjacent links with 1
func nodeReplace2Kid(new BNode, old BNode, idx uint16, ptr uint64, key []byte) {
	new.setHeader(BNODE_NODE, old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+2, old.nkeys()-(idx+2))
	new.setPtr(idx, ptr)
	new.setKey(idx, key)
}

func (node BNode) setKey(idx uint16, key []byte) {
	pos := node.kvPos(idx)
	klen := uint16(len(key))
	binary.LittleEndian.PutUint16(node[pos:], klen)
	copy(node[pos+4:], key)
}

//============================== MERGE CONDITIONS =======================

// should the updated kid be merged with a siblinf

func shouldMerge(tree *BTree, node BNode, idx uint16, updated BNode) (int, BNode) {
	if updated.nbytes() > BTREE_PAGE_SIZE/4 {
		return 0, BNode{}

	}

	if idx > 0 {
		sibling := BNode(tree.get(node.getPtr(idx - 1)))
		merged := sibling.nbytes() + updated.nbytes() - HEADER

		if merged <= BTREE_PAGE_SIZE {
			return -1, sibling // left
		}
	}

	if idx+1 < node.nkeys() {
		sibling := BNode(tree.get(node.getPtr(idx + 1)))
		merged := sibling.nbytes() + updated.nbytes() - HEADER

		if merged <= BTREE_PAGE_SIZE {
			return +1, sibling // right
		}

	}

	return 0, BNode{}
}

// delete a key from the tree
func treeDelete(tree *BTree, node BNode, key []byte) BNode {
	// The result node. It's allowed to be oversized and will be handled by the parent.
	new := BNode(make([]byte, 2*BTREE_PAGE_SIZE))

	// Find the position to delete
	idx := nodeLookUpLE(node, key)

	switch node.btype() {
	case BNODE_LEAF:
		// Handle leaf node deletion
		if !bytes.Equal(key, node.getKey(idx)) {
			// Key not found
			return BNode{}
		}

		// Delete the key from the leaf
		leafDelete(new, node, idx)

	case BNODE_NODE:
		// Handle internal node deletion (recursive)
		return nodeDelete(tree, node, idx, key)

	default:
		panic("bad node!")
	}

	return new
}

// delete a key from an internal node; part of the treeDelete()
func nodeDelete(tree *BTree, node BNode, idx uint16, key []byte) BNode {
	// recurse into the kid
	kptr := node.getPtr(idx)
	updated := treeDelete(tree, tree.get(kptr), key)

	if len(updated) == 0 {
		return BNode{} // not found
	}
	tree.del(kptr)

	new := BNode(make([]byte, BTREE_PAGE_SIZE))

	// check for mergin
	mergeDir, sibling := shouldMerge(tree, node, idx, updated)

	switch {
	case mergeDir < 0: // left
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, sibling, updated)
		tree.del(node.getPtr(idx - 1))
		nodeReplace2Kid(new, node, idx-1, tree.new(merged), merged.getKey(0))
	case mergeDir > 0: // right
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, updated, sibling)
		tree.del(node.getPtr(idx + 1))
		nodeReplace2Kid(new, node, idx, tree.new(merged), merged.getKey(0))
	case mergeDir == 0 && updated.nkeys() == 0:
		assert(node.nkeys() == 1 && idx == 0) // 1 empty child but no sibling
		new.setHeader(BNODE_NODE, 0)          // the parent becomes empty too
	case mergeDir == 0 && updated.nkeys() > 0: // no merge
		nodeReplaceKidN(tree, new, node, idx, updated)
	}

	return new
}

// ==================  TEST THE B+TREE ====================== ////////

type C struct {
	tree  BTree
	ref   map[string]string // the reference data
	pages map[uint64]BNode  // in-memory pages
}

func newC() *C {
	pages := map[uint64]BNode{}
	return &C{
		tree: BTree{
			get: func(ptr uint64) []byte {
				node, ok := pages[ptr]
				assert(ok)
				return node
			},
			new: func(node []byte) uint64 {
				assert(BNode(node).nbytes() <= BTREE_PAGE_SIZE)
				ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
				assert(pages[ptr] == nil)
				pages[ptr] = node
				return ptr
			},
			del: func(ptr uint64) {
				assert(pages[ptr] != nil)
				delete(pages, ptr)
			},
		},

		ref:   map[string]string{},
		pages: pages,
	}
}

func (c *C) add(key string, val string) {
	c.tree.Insert([]byte(key), []byte(val))
	c.ref[key] = val // reference data
}
