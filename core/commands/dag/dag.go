package dagcmd

import (
	"fmt"
	"io"
	"math"

	"github.com/ipsn/go-ipfs/core/commands/cmdenv"
	"github.com/ipsn/go-ipfs/core/commands/e"
	"github.com/ipsn/go-ipfs/core/coredag"
	"github.com/ipsn/go-ipfs/pin"

	cid "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-cid"
	mh "github.com/ipsn/go-ipfs/gxlibs/github.com/multiformats/go-multihash"
	path "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-path"
	files "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-files"
	ipld "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipld-format"
	cmds "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-cmds"
	cmdkit "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-cmdkit"
)

var DagCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Interact with ipld dag objects.",
		ShortDescription: `
'ipfs dag' is used for creating and manipulating dag objects.

This subcommand is currently an experimental feature, but it is intended
to deprecate and replace the existing 'ipfs object' command moving forward.
		`,
	},
	Subcommands: map[string]*cmds.Command{
		"put":     DagPutCmd,
		"get":     DagGetCmd,
		"resolve": DagResolveCmd,
	},
}

// OutputObject is the output type of 'dag put' command
type OutputObject struct {
	Cid cid.Cid
}

// ResolveOutput is the output type of 'dag resolve' command
type ResolveOutput struct {
	Cid     cid.Cid
	RemPath string
}

var DagPutCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Add a dag node to ipfs.",
		ShortDescription: `
'ipfs dag put' accepts input from a file or stdin and parses it
into an object of the specified format.
`,
	},
	Arguments: []cmdkit.Argument{
		cmdkit.FileArg("object data", true, true, "The object to put").EnableStdin(),
	},
	Options: []cmdkit.Option{
		cmdkit.StringOption("format", "f", "Format that the object will be added as.").WithDefault("cbor"),
		cmdkit.StringOption("input-enc", "Format that the input object will be.").WithDefault("json"),
		cmdkit.BoolOption("pin", "Pin this object when adding."),
		cmdkit.StringOption("hash", "Hash function to use").WithDefault(""),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		ienc, _ := req.Options["input-enc"].(string)
		format, _ := req.Options["format"].(string)
		hash, _ := req.Options["hash"].(string)
		dopin, _ := req.Options["pin"].(bool)

		// mhType tells inputParser which hash should be used. MaxUint64 means 'use
		// default hash' (sha256 for cbor, sha1 for git..)
		mhType := uint64(math.MaxUint64)

		if hash != "" {
			var ok bool
			mhType, ok = mh.Names[hash]
			if !ok {
				return fmt.Errorf("%s in not a valid multihash name", hash)
			}
		}

		outChan := make(chan interface{}, 8)

		addAllAndPin := func(f files.File) error {
			cids := cid.NewSet()
			b := ipld.NewBatch(req.Context, nd.DAG)

			for {
				file, err := f.NextFile()
				if err == io.EOF {
					// Finished the list of files.
					break
				} else if err != nil {
					return err
				}

				nds, err := coredag.ParseInputs(ienc, format, file, mhType, -1)
				if err != nil {
					return err
				}
				if len(nds) == 0 {
					return fmt.Errorf("no node returned from ParseInputs")
				}

				for _, nd := range nds {
					err := b.Add(nd)
					if err != nil {
						return err
					}
				}

				cid := nds[0].Cid()
				cids.Add(cid)

				select {
				case outChan <- &OutputObject{Cid: cid}:
				case <-req.Context.Done():
					return nil
				}
			}

			if err := b.Commit(); err != nil {
				return err
			}

			if dopin {
				defer nd.Blockstore.PinLock().Unlock()

				cids.ForEach(func(c cid.Cid) error {
					nd.Pinning.PinWithMode(c, pin.Recursive)
					return nil
				})

				err := nd.Pinning.Flush()
				if err != nil {
					return err
				}
			}

			return nil
		}

		errC := make(chan error)
		go func() {
			var err error
			defer func() { errC <- err }()
			defer close(outChan)
			err = addAllAndPin(req.Files)
		}()

		err = res.Emit(outChan)
		if err != nil {
			return err
		}

		return <-errC
	},
	Type: OutputObject{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, out *OutputObject) error {
			fmt.Fprintln(w, out.Cid.String())
			return nil
		}),
	},
}

var DagGetCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Get a dag node from ipfs.",
		ShortDescription: `
'ipfs dag get' fetches a dag node from ipfs and prints it out in the specified
format.
`,
	},
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("ref", true, false, "The object to get").EnableStdin(),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		p, err := path.ParsePath(req.Arguments[0])
		if err != nil {
			return err
		}

		lastCid, rem, err := nd.Resolver.ResolveToLastNode(req.Context, p)
		if err != nil {
			return err
		}
		obj, err := nd.DAG.Get(req.Context, lastCid)
		if err != nil {
			return err
		}

		var out interface{} = obj
		if len(rem) > 0 {
			final, _, err := obj.Resolve(rem)
			if err != nil {
				return err
			}
			out = final
		}
		return res.Emit(&out)
	},
}

// DagResolveCmd returns address of highest block within a path and a path remainder
var DagResolveCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Resolve ipld block",
		ShortDescription: `
'ipfs dag resolve' fetches a dag node from ipfs, prints it's address and remaining path.
`,
	},
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("ref", true, false, "The path to resolve").EnableStdin(),
	},
	Run: func(req *cmds.Request, res cmds.ResponseEmitter, env cmds.Environment) error {
		nd, err := cmdenv.GetNode(env)
		if err != nil {
			return err
		}

		p, err := path.ParsePath(req.Arguments[0])
		if err != nil {
			return err
		}

		lastCid, rem, err := nd.Resolver.ResolveToLastNode(req.Context, p)
		if err != nil {
			return err
		}

		return res.Emit(&ResolveOutput{
			Cid:     lastCid,
			RemPath: path.Join(rem),
		})
	},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeTypedEncoder(func(req *cmds.Request, w io.Writer, out *ResolveOutput) error {
			p := out.Cid.String()
			if out.RemPath != "" {
				p = path.Join([]string{p, out.RemPath})
			}

			fmt.Fprint(w, p)
			return nil
		}),
	},
	Type: ResolveOutput{},
}

// copy+pasted from ../commands.go
func unwrapOutput(i interface{}) (interface{}, error) {
	var (
		ch <-chan interface{}
		ok bool
	)

	if ch, ok = i.(<-chan interface{}); !ok {
		return nil, e.TypeErr(ch, i)
	}

	return <-ch, nil
}
