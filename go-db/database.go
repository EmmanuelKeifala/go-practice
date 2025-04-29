package main

import (
	"bytes"
	"encoding/binary"
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
func (node BNode) setPtr(idx uint16, val uint64)

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
func (node BNode) setOffset(idx uint16, offset uint16)

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

func (node BNode) getVal(idx uint16) []byte

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
func nodeAppendRange(new BNode, old BNode, dstNew uint16, srcOld uint16, n uint16)

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
func (trr *BTree) Delete(key []byte) bool

// remove a key from a leaf node
func leafDelete(new BNode, old BNode, idx uint16)

// merge 2 nodes into 1
func nodeMerge(new BNode, left BNode, right BNode)

// replace 2 adjacent links with 1
func nodeReplace2Kid(new BNode, old BNode, idx uint16, ptr uint64, key []byte)

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
