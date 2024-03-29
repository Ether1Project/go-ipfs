package coreunix

import (
	"bytes"
	"context"
	"io/ioutil"
	"testing"

	core "github.com/ipsn/go-ipfs/core"
	bserv "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-blockservice"
	ft "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-unixfs"
	importer "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-unixfs/importer"
	uio "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-unixfs/io"
	merkledag "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-merkledag"

	u "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-util"
	chunker "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-chunker"
	cid "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-cid"
	bstore "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-blockstore"
	offline "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-exchange-offline"
	ipld "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipld-format"
	ds "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-datastore"
	dssync "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-datastore/sync"
)

func getDagserv(t *testing.T) ipld.DAGService {
	db := dssync.MutexWrap(ds.NewMapDatastore())
	bs := bstore.NewBlockstore(db)
	blockserv := bserv.New(bs, offline.Exchange(bs))
	return merkledag.NewDAGService(blockserv)
}

func TestMetadata(t *testing.T) {
	ctx := context.Background()
	// Make some random node
	ds := getDagserv(t)
	data := make([]byte, 1000)
	u.NewTimeSeededRand().Read(data)
	r := bytes.NewReader(data)
	nd, err := importer.BuildDagFromReader(ds, chunker.DefaultSplitter(r))
	if err != nil {
		t.Fatal(err)
	}

	c := nd.Cid()

	m := new(ft.Metadata)
	m.MimeType = "THIS IS A TEST"

	// Such effort, many compromise
	ipfsnode := &core.IpfsNode{DAG: ds}

	mdk, err := AddMetadataTo(ipfsnode, c.String(), m)
	if err != nil {
		t.Fatal(err)
	}

	rec, err := Metadata(ipfsnode, mdk)
	if err != nil {
		t.Fatal(err)
	}
	if rec.MimeType != m.MimeType {
		t.Fatalf("something went wrong in conversion: '%s' != '%s'", rec.MimeType, m.MimeType)
	}

	cdk, err := cid.Decode(mdk)
	if err != nil {
		t.Fatal(err)
	}

	retnode, err := ds.Get(ctx, cdk)
	if err != nil {
		t.Fatal(err)
	}

	rtnpb, ok := retnode.(*merkledag.ProtoNode)
	if !ok {
		t.Fatal("expected protobuf node")
	}

	ndr, err := uio.NewDagReader(ctx, rtnpb, ds)
	if err != nil {
		t.Fatal(err)
	}

	out, err := ioutil.ReadAll(ndr)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(out, data) {
		t.Fatal("read incorrect data")
	}
}
