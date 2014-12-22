package main

import (
	"os"
	"fmt"
	"strings"
	"flag"
	"net"
	"github.com/dpw/go-openvswitch/openvswitch"
)

func printErr(f string, a ...interface{}) bool {
	fmt.Fprintf(os.Stderr, f, a...)
	os.Stderr.WriteString("\n")
	return false
}

type commandDispatch interface {
	run(args []string, pos int) bool
}


type subcommands map[string]commandDispatch

func (cmds subcommands) run(args []string, pos int) bool {
	if pos >= len(args) {
		return printErr("Subcommand required by \"%s\".  Try \"%s help\"", strings.Join(args[:pos], " "), args[0])
	}

	cd, ok := cmds[args[pos]]

	if !ok {
		return printErr("Unknown command \"%s\".  Try \"%s help\"", strings.Join(args[:pos + 1], " "), args[0])
	}

	return cd.run(args, pos + 1)
}


type Flags struct {
	*flag.FlagSet
	args []string
}

func (f Flags) Parse() {
	f.FlagSet.Parse(f.args)
}

func (f Flags) CheckNArg(min int, max int) bool {
	if f.NArg() < min {
		return printErr("Insufficient arguments")
	}

	if f.NArg() > max {
		return printErr("Excess arguments")
	}

	return true
}

type command func (f Flags) bool

func (fcmd command) run(args []string, pos int) bool {
	f := flag.NewFlagSet(strings.Join(args[:pos], " "), flag.ExitOnError)
	return fcmd(Flags{f, args[pos:]})
}


type possibleSubcommands struct {
	command command
	subcommands subcommands
}

func (cmds possibleSubcommands) run(args []string, pos int) bool {
	if pos >= len(args) {
		return cmds.command.run(args, pos)
	}

	return cmds.subcommands.run(args, pos)
}


var commands = subcommands {
	"datapath": possibleSubcommands {
		listDatapaths,
		subcommands {
			"create": command(createDatapath),
			"delete": command(deleteDatapath),
		},
	},
	"vport": subcommands {
		"create": subcommands {
			"internal": command(createInternalVport),
			"vxlan" : command(createVxlanVport),
		},
		"delete": command(deleteVport),
		"list": command(listVports),
	},
	"flow": subcommands {
		"create": command(createFlow),
		"delete": command(deleteFlow),
		"list": command(listFlows),
	},
}

func main() {
	if (!commands.run(os.Args, 1)) {
		os.Exit(1)
	}
}

func createDatapath(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(1, 1) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	_, err = dpif.CreateDatapath(f.Arg(0))
	if err != nil { return printErr("%s", err) }

	return true
}

func deleteDatapath(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(1, 1) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	name := f.Arg(0)
	dp, err := dpif.LookupDatapath(name)
	if err != nil { return printErr("%s", err) }

	if openvswitch.IsNoSuchDatapathError(err) {
		return printErr("Cannot find datapath \"%s\"", name);
	}

	err = dp.Delete()
	if err != nil { return printErr("%s", err) }

	return true
}

func listDatapaths(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(0, 0) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dps, err := dpif.EnumerateDatapaths()
	for name := range(dps) {
		fmt.Printf("%s\n", name)
	}

	return true
}

func createInternalVport(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(2, 2) { return false }
	return createVport(f.Arg(0), openvswitch.NewInternalVportSpec(f.Arg(1)))
}

func createVxlanVport(f Flags) bool {
	var destPort uint
	// default taken from ovs/lib/netdev-vport.c
	f.UintVar(&destPort, "destport", 4789, "destination UDP port number")
	f.Parse()
	if !f.CheckNArg(2, 2) { return false }

	if destPort > 65535 {
		return printErr("destport too large")
	}

	return createVport(f.Arg(0), openvswitch.NewVxlanVportSpec(f.Arg(1), uint16(destPort)))
}

func createVport(dpname string, spec openvswitch.VportSpec) bool {
	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(dpname)
	if err != nil { return printErr("%s", err) }

	_, err = dp.CreateVport(spec)
	if err != nil { return printErr("%s", err) }

	return true
}

func deleteVport(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(1, 1) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	name := f.Arg(0)
	vport, err := dpif.LookupVport(name)
	if err != nil {
		if openvswitch.IsNoSuchVportError(err) {
			return printErr("Cannot find port \"%s\"", name);
		}

		return printErr("%s", err)
	}

	err = vport.Handle.Delete()
	if err != nil { return printErr("%s", err) }

	return true
}

func listVports(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(1, 1) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(f.Arg(0))
	if err != nil { return printErr("%s", err) }

	vports, err := dp.EnumerateVports()
	for _, vport := range(vports) {
		spec := vport.Spec
		fmt.Printf("%s %s", spec.TypeName(), spec.Name())

		switch spec := spec.(type) {
		case openvswitch.VxlanVportSpec:
			fmt.Printf(" --destport=%d", spec.DestPort)
			break
		}

		fmt.Printf("\n")
	}

	return true
}

func parseMAC(s string) (mac [6]byte, err error) {
	hwa, err := net.ParseMAC(s)
	if err != nil { return }

	if len(hwa) != 6 {
		err = fmt.Errorf("invalid MAC address: %s", s)
		return
	}

	copy(mac[:], hwa)
	return
}

func flagsToFlowSpec(f Flags) (openvswitch.FlowSpec, bool) {
	flow := openvswitch.NewFlowSpec()

	var ethSrc, ethDst string
	f.StringVar(&ethSrc, "ethsrc", "", "ethernet source MAC")
	f.StringVar(&ethDst, "ethdst", "", "ethernet destination MAC")
	f.Parse()
	if !f.CheckNArg(1, 1) { return flow, false }

	if (ethSrc != "") != (ethDst != "") {
		return flow, printErr("Must supply both 'ethsrc' and 'ethdst' options")
	}

	if ethSrc != "" {
		src, err := parseMAC(ethSrc)
		if err != nil { return flow, printErr("%s", err) }
		dst, err := parseMAC(ethDst)
		if err != nil { return flow, printErr("%s", err) }

		flow.AddKey(openvswitch.NewEthernetFlowKey(src, dst))
	}

	return flow, true
}

func createFlow(f Flags) bool {
	flow, ok := flagsToFlowSpec(f)
	if !ok { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(f.Arg(0))
	if err != nil { return printErr("%s", err) }

	err = dp.CreateFlow(flow)
	if err != nil { return printErr("%s", err) }

	return true
}

func deleteFlow(f Flags) bool {
	flow, ok := flagsToFlowSpec(f)
	if !ok { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(f.Arg(0))
	if err != nil { return printErr("%s", err) }

	err = dp.DeleteFlow(flow)
	if err != nil { return printErr("%s", err) }

	return true
}

func listFlows(f Flags) bool {
	f.Parse()
	if !f.CheckNArg(1, 1) { return false }

	dpif, err := openvswitch.NewDpif()
	if err != nil { return printErr("%s", err) }
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(f.Arg(0))
	if err != nil { return printErr("%s", err) }

	flows, err := dp.EnumerateFlows()
	if err != nil { return printErr("%s", err) }

	for _, flow := range(flows) {
		if !printFlow(f.Arg(0), flow) { return false }
	}

	return true
}

func printFlow(dpname string, flow openvswitch.FlowSpec) bool {
	os.Stdout.WriteString(dpname)

	for _, fk := range(flow.FlowKeys) {
		if fk.Ignored() { continue }

		switch fk := fk.(type) {
		case openvswitch.EthernetFlowKey:
			s := fk.EthSrc()
			d := fk.EthDst()
			fmt.Printf(" --ethsrc=%s --ethdst=%s",
				net.HardwareAddr(s[:]),
				net.HardwareAddr(d[:]))
			break

		default:
			fmt.Printf("%v", fk)
			break
		}
	}

	os.Stdout.WriteString("\n")
	return true
}
