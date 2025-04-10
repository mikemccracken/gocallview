package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	//"github.com/olekukonko/tablewriter"

	"go/token"

	//"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	//"golang.org/x/tools/go/callgraph/rta"
	//"golang.org/x/tools/go/callgraph/static"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/rivo/tview"
	"github.com/urfave/cli/v2"
)

const HelpText = `
[blue]# Command Reference[white]
=================

## Overall

ctrl-q: exit
ctrl-h: show / hide help message
ctrl-s: focus search text field
ctrl-f: expand text pane to full screen with no borders (nice for term copy)

## Tree navigation

K / left-arrow: go to parent
right-arrow: go to next row, whether child or sibling
up-arrow: go to previous sibling
down-arrow: go to next sibling or a child if no sibling exists

enter: hide/show subtree, expanding if necessary
`

var GlobalFileSet *token.FileSet

// how deep we expand the paths of a tree at each step
const ExpandSize = 4

var CurrentSearchNeedle = ""

// how many lines of text context on either side of the line
var contextSize = 2

func getFileContext(filename string, linenum int) string {
	file, err := os.Open(filename)
	if err != nil {
		return "err"
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	s := "\n"

	startLine := max(1, linenum-contextSize)

	for idx := 1; idx <= linenum+contextSize; idx++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("err reading %v\n", err)
			return s
		}
		if idx >= startLine {
			cstart := "[blue]"
			if idx == linenum {
				cstart = "[yellow]"
			}
			s = fmt.Sprintf("%s%d%s %s[white]", s, idx, cstart, line)
		}
	}
	return s
}

func edgeSummary(path []*callgraph.Edge, showContext bool) string {
	s := ""
	spc := ""

	for _, edge := range path {
		pos := edge.Callee.Func.Pos()
		function := edge.Callee.Func

		if edge.Site != nil {
			pos = edge.Site.Pos()
			function = edge.Site.Parent()
		}
		position := function.Prog.Fset.Position(pos)
		callerStr := "(none)"
		if edge.Caller != nil {
			callerStr = edge.Caller.Func.String() //.Name()
		}
		calleeStr := edge.Callee.Func.String() //.Name()
		s = fmt.Sprintf("%s%s[%s -> %s](%s:%d)\n", s, spc, callerStr, calleeStr, position.Filename, position.Line)
		if showContext {
			fileContext := getFileContext(position.Filename, position.Line)
			s = fmt.Sprintf("%s\n```%s```", s, fileContext)
		}
		spc = "\n\n"
	}
	return s
}

const pkgLoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedExportsFile |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedTypesSizes |
	packages.NeedModule

func getCallGraph(packageNames []string) (*callgraph.Graph, []*packages.Package, error) {

	cfg := &packages.Config{
		Mode:  pkgLoadMode,
		Tests: false,
		Dir:   "",
	}

	initial, err := packages.Load(cfg, packageNames...)
	if err != nil {
		return nil, nil, err
	}
	if packages.PrintErrors(initial) > 0 {
		log.Printf("some errors were had")
		packages.Visit(initial, nil, func(pkg *packages.Package) {
			log.Printf("go mod for pkg %v is %v", pkg, pkg.Module.GoMod)
			for _, err := range pkg.Errors {
				log.Printf("%v", err)
			}
		})
		//return nil, nil, fmt.Errorf("packages contain errors")
	}

	mode := ssa.InstantiateGenerics
	prog, _ := ssautil.AllPackages(initial, mode)
	//prog, _ := ssautil.Packages(initial, mode)
	prog.Build()
	GlobalFileSet = prog.Fset
	cg := vta.CallGraph(ssautil.AllFunctions(prog), cha.CallGraph(prog))

	cg.DeleteSyntheticNodes()

	return cg, initial, nil
}

func addNode(t *tview.TreeNode, label string, r interface{}) *tview.TreeNode {
	n := tview.NewTreeNode("placeholder")
	n.SetSelectable(true)
	n.SetText(label)
	n.SetReference(r)
	t.AddChild(n)
	return n
}

func getAllChildren(node *tview.TreeNode) []*tview.TreeNode {
	var children []*tview.TreeNode
	for _, child := range node.GetChildren() {
		children = append(children, child)
		children = append(children, getAllChildren(child)...)
	}
	return children
}

func getMatchingTreeNodes(node *tview.TreeNode, needle string) []*tview.TreeNode {
	reference := node.GetReference()
	thisNodeMatches := false

	// reference nil is the root, doesn't match anything
	if reference != nil {
		var haystacks []string
		switch ref := reference.(type) {
		case []*callgraph.Edge:
			haystacks = []string{edgeSummary(ref, false)}
		default:
			log.Printf("getmatching: unknown type for reference %v: %T", reference, reference)
		}

		for _, haystack := range haystacks {
			if strings.Contains(haystack, needle) {
				thisNodeMatches = true
			}
		}
	}

	if thisNodeMatches {
		ret := []*tview.TreeNode{node}
		allChildren := getAllChildren(node)
		return append(ret, allChildren...)
	}

	// if this node doesn't match, still return it if a child matches:
	children := node.GetChildren()
	var childMatches []*tview.TreeNode
	if len(children) > 0 {
		for _, child := range children {
			childMatches = append(childMatches, getMatchingTreeNodes(child, needle)...)
		}
	}

	if len(childMatches) > 0 {
		ret := []*tview.TreeNode{node}
		return append(ret, childMatches...)
	}

	return []*tview.TreeNode{}
}

func clearTreeFormatting(node *tview.TreeNode, selectable bool) {

	if selectable {
		reference := node.GetReference()
		if reference != nil {
			switch reference.(type) {
			case []*callgraph.Edge:
				node.SetColor(tcell.ColorBlue)
			default:
				log.Printf("unknown type for reference %v: %T", reference, reference)
			}
		}

	} else {
		node.SetColor(tcell.ColorGray)

	}
	node.SetSelectable(selectable)
	children := node.GetChildren()
	if len(children) > 0 {
		for _, child := range children {
			clearTreeFormatting(child, selectable)
		}
	}
}

func filenameForFunc(function *ssa.Function) string {
	return function.Prog.Fset.Position(function.Pos()).Filename
}

var CurrentMatchingNodes = make(map[*callgraph.Node]bool)
var nodesSearched = 0

func markMatchingCalleesOfNode(n *callgraph.Node, seen map[*callgraph.Node]bool, expandStdlib bool, needle string, debug bool) (bool, error) {
	anyCalleeMatches := false
	nodesSearched += 1
	for _, e := range n.Out {
		if !expandStdlib && strings.HasPrefix(filenameForFunc(e.Callee.Func), "/usr/local/go") {
			continue
		}

		if e.Callee.Func.Name() == needle {
			CurrentMatchingNodes[e.Callee] = true
			return true, nil
		}
		if !seen[e.Callee] {
			seen[e.Callee] = true
			foundMatch, err := markMatchingCalleesOfNode(e.Callee, seen, expandStdlib, needle, debug)
			if err != nil {
				return false, err
			}
			delete(seen, e.Callee)
			if foundMatch {
				anyCalleeMatches = true
			}
		}

		if anyCalleeMatches {
			CurrentMatchingNodes[e.Callee] = true
		}
	}
	return anyCalleeMatches, nil
}

func expandCalleesOfNode(n *callgraph.Node, prevTreeNode *tview.TreeNode, path []*callgraph.Edge,
	seen map[*callgraph.Node]bool, expandSize int, expandStdlib bool, debug bool) error {

	if debug {
		log.Printf("DEBUG: expandCalleesOfNode with n=%+v and prevTreeNode=%+v", n, prevTreeNode)
	}

	for _, e := range n.Out {
		// when searching, do not limit expand depth
		if CurrentSearchNeedle == "" {
			if len(path) == expandSize {
				continue
			}
		} else {
			if !CurrentMatchingNodes[e.Callee] {
				continue
			}
		}

		if !expandStdlib && strings.HasPrefix(filenameForFunc(e.Callee.Func), "/usr/local/go") {
			continue
		}

		path = append(path, e)

		nodeName := fmt.Sprintf("%s", e.Callee.Func.String())
		if len(path) == expandSize {
			nodeName = fmt.Sprintf("%s ...", nodeName)
		}
		pathcopy := append(make([]*callgraph.Edge, 0, len(path)), path...)
		newNode := addNode(prevTreeNode, nodeName, pathcopy)
		if debug {
			log.Printf("created new node %+v for name %q", newNode, nodeName)
		}
		if !seen[e.Callee] {
			seen[e.Callee] = true
			err := expandCalleesOfNode(e.Callee, newNode, path, seen, expandSize, expandStdlib, debug)
			if err != nil {
				return err
			}
			delete(seen, e.Callee)
		}
		path = path[:len(path)-1]

	}

	return nil
}

func doTViewStuff(ctxt *cli.Context) error {

	packages := ctxt.Args().Slice()
	log.Print(packages)
	if len(packages) == 0 {
		packages = []string{"./..."}
	}

	app := tview.NewApplication()

	root := tview.NewTreeNode("Functions").
		SetColor(tcell.ColorRed)

	tree := tview.NewTreeView().
		SetRoot(root).
		SetCurrentNode(root).SetAlign(false).SetTopLevel(1).SetGraphics(true)
	tree.Box.SetBorder(true)

	searchInputField := tview.NewInputField()
	searchInputField.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(tree)
	})

	searchInputField.Box.SetBorder(true)

	treeGrid := tview.NewGrid().SetRows(0, 3).SetColumns(0).
		AddItem(tree, 0, 0, 1, 1, 0, 0, true).
		AddItem(searchInputField, 1, 0, 1, 1, 0, 0, false)

	cg, initialpackages, err := getCallGraph(packages)
	if err != nil {
		log.Printf("error in getCallGraph: %+v", err)
		return err
	}
	initialPackagePathSet := make(map[string]bool)
	for _, pkg := range initialpackages {
		initialPackagePathSet[pkg.PkgPath] = true
	}
	log.Printf("got Callgraph, moving on to creating tree\n")
	nRoots := 0

	CreateTree := func() {
		// seen is per path from root, so we visit commonly called functions many
		// times because we want a new node every time it is called on a unique path
		seen := make(map[*callgraph.Node]bool)

		root.ClearChildren()

		// Nodes contains all nodes, but we want to start at roots in our initial
		// set of packages only. Initially we have a small max path length and can
		// expand it by calling expandCaleesOfNode again with a larger value
		//
		// TODO: sort the root nodes somehow
		for _, node := range cg.Nodes {
			if len(node.In) != 0 {
				continue
			}
			if node.Func.Name() == "init" {
				continue
			}
			if _, ok := initialPackagePathSet[node.Func.Pkg.Pkg.Path()]; !ok {
				continue
			}
			nRoots++

			fakeEdge := callgraph.Edge{
				Callee: node,
			}
			path := []*callgraph.Edge{&fakeEdge}
			rootFuncNode := addNode(root, node.Func.String(), path)
			seen[node] = true
			// Initial expandsize plus one to account for the fake path to root
			shouldExpandStdlib := false // TODO make dynamic
			log.Printf("expanding callees of root node %q", node.Func.String())
			err := expandCalleesOfNode(node, rootFuncNode, path, seen, ExpandSize+1, shouldExpandStdlib, false)
			if err != nil {
				log.Printf("error visiting %+v", node)
			}
			// TODO: maybe remove root if none were found?
		}

	}

	markMatchingRootNodes := func(needle string) {
		// seen is per path from root, so we visit commonly called functions many
		// times because we want a new node every time it is called on a unique path
		seen := make(map[*callgraph.Node]bool)

		// Nodes contains all nodes, but we want to start at roots in our initial
		// set of packages only.
		for _, node := range cg.Nodes {
			if len(node.In) != 0 {
				continue
			}
			if node.Func.Name() == "init" {
				continue
			}
			if _, ok := initialPackagePathSet[node.Func.Pkg.Pkg.Path()]; !ok {
				continue
			}
			if node.Func.Name() == needle {
				log.Printf("mark: found needle %q", needle)
				CurrentMatchingNodes[node] = true
			}

			seen[node] = true
			log.Printf("mark: searching callees of root node %q", node.Func.String())
			shouldExpandStdlib := false // todo config
			anyCalleesMatch, err := markMatchingCalleesOfNode(node, seen, shouldExpandStdlib, needle, false)
			if err != nil {
				log.Printf("error marking %+v", node)
			}
			if anyCalleesMatch {
				CurrentMatchingNodes[node] = true
			}
		}
	}

	log.Printf("markMatchingNodes start test hardcoded")
	markMatchingRootNodes("doTViewStuff")
	log.Printf("markMatchingNodes end, len(CurrentMatchingNodes)=%d nodesSearched=%d", len(CurrentMatchingNodes), nodesSearched)

	// TODO: maybe allow cli arg for needle
	CreateTree()

	searchInputField.SetLabel(fmt.Sprintf("Search all callsites (%d roots): ", nRoots))

	searchInputField.SetChangedFunc(func(needle string) {
		log.Printf("in SetChangedFunc needle is %q", needle)
		CreateTree()

		if needle == "" {
			tree.SetCurrentNode(nil)
			clearTreeFormatting(root, true)
			return
		}

		// look through all tree children and highlight ones that match, and
		// autoselect the first match that is an oci layout

		// matches := getMatchingTreeNodes(root, needle)
		// if len(matches) == 0 {
		// 	clearTreeFormatting(root, true)
		// } else {
		// 	// set everything unselectable to allow us to just set the matches selectable
		// 	clearTreeFormatting(root, false)

		// 	firstLayoutNodeIdx := 0

		// 	for _, match := range matches {

		// 		match.SetColor(tcell.ColorYellow)
		// 		match.SetSelectable(true)
		// 	}

		// 	tree.SetCurrentNode(matches[firstLayoutNodeIdx])
		// 	// force a process() call
		// 	tree.Move(1)
		// 	tree.Move(-1)

		// }
		// // update info pane with summaries
	})
	summaries := []string{}

	clearTreeFormatting(root, true)
	infoPane := tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetText(strings.Join(summaries, "\n")).
		SetDynamicColors(true).
		SetRegions(true)
	infoPane.Box.SetBorder(true)

	statusLineDefaultText := "press 'ctrl-h' to show help, 'ctrl-s' to search or 'ctrl-q' to exit"
	statusLine := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText(statusLineDefaultText)

	treeselfunc := func(node *tview.TreeNode) {
		reference := node.GetReference()
		if reference == nil {
			infoPane.SetText(strings.Join(summaries, "\n"))
			infoPane.ScrollToEnd()
			return
		}
		children := node.GetChildren()
		if len(children) == 0 {
			switch ref := reference.(type) {
			case []*callgraph.Edge:
				infoPane.SetText(edgeSummary(ref, true))
				infoPane.ScrollToEnd()
			default:
				log.Printf("node ref is unknown type: %T\n", reference)
			}
		} else {
			switch ref := reference.(type) {
			case []*callgraph.Edge:
				infoPane.SetText(edgeSummary(ref, true))
				infoPane.ScrollToEnd()
			default:
				log.Printf("node ref is unknown type: %T\n", reference)
				infoPane.SetText("error")
			}
		}
	}
	tree.SetSelectedFunc(treeselfunc)
	tree.SetChangedFunc(treeselfunc)

	// work around missing GetParent()
	getParentOfTreeNode := func(root, n *tview.TreeNode) *tview.TreeNode {
		// build tree until n
		var parent *tview.TreeNode
		root.Walk(func(node, p *tview.TreeNode) bool {
			if node == n {
				parent = p
				return false
			}
			return true
		})
		return parent
	}

	// customise the movement keys, fall back to default treeview behavior by returning event
	tree.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		cur := tree.GetCurrentNode()
		switch key := event.Key(); key {
		case tcell.KeyDown:
			// down skipping children
			// treeChild movement from treeView is weird.
			if cur == nil {
				return event
			}
			curParent := getParentOfTreeNode(root, cur)
			if curParent == nil {
				return event
			}
			curParentChildren := curParent.GetChildren()

			nextidx := slices.Index(curParentChildren, cur) + 1
			if nextidx < len(curParentChildren) {
				tree.SetCurrentNode(curParentChildren[nextidx])
				treeselfunc(tree.GetCurrentNode())
			} else {
				// if I am the last of my parent's children, use default behavior to just go to next
				return event
			}
			return nil

		case tcell.KeyUp:
			// want previous in list of parent's children, not just the previous row
			curParent := getParentOfTreeNode(root, cur)
			if curParent == nil {
				return event
			}
			curParentChildren := curParent.GetChildren()
			previdx := slices.Index(curParentChildren, cur) - 1
			if previdx >= 0 {
				tree.SetCurrentNode(curParentChildren[previdx])
				treeselfunc(tree.GetCurrentNode())
			} else {
				return event
			}
			return nil

		case tcell.KeyLeft:
			// want behavior of treeParent movement from treeView, 'K'
			curParent := getParentOfTreeNode(root, cur)
			if curParent == nil {
				return event
			}
			tree.SetCurrentNode(curParent)
			treeselfunc(tree.GetCurrentNode())
			return nil

		case tcell.KeyRune:
			if r := event.Rune(); key == tcell.KeyRune {
				switch r {
				case 'h':
					log.Printf("Hide selected.")
				}
				return event
			}
		case tcell.KeyEnter:
			// enter toggles expanded setting

			if cur == nil {
				return event
			}

			// TODO why does this need two enters
			// TODO update the string

			// if we hit enter on a node that has no children, see if we can expand it some
			if len(cur.GetChildren()) == 0 {
				reference := cur.GetReference()
				if reference == nil {
					log.Printf("weird, got nil reference for node: %v", cur)
					return nil
				}
				switch ref := reference.(type) {
				case []*callgraph.Edge:
					callgraphNode := ref[len(ref)-1].Callee
					generatedSeen := make(map[*callgraph.Node]bool)
					for _, edge := range ref {
						generatedSeen[edge.Callee] = true
					}
					statusLine.SetText(fmt.Sprintf("expanding node %q", callgraphNode.String()))
					shouldExpandStdlib := false //TODO make this dynamic
					expandCalleesOfNode(callgraphNode, cur, ref, generatedSeen, len(ref)+ExpandSize, shouldExpandStdlib, false)
					statusLine.SetText(statusLineDefaultText)
					clearTreeFormatting(root, true)
				default:
					log.Printf("node ref is unknown type: %T\n", reference)
				}
				infoPane.ScrollToEnd()
				cur.SetExpanded(true)
			} else {
				cur.SetExpanded(!cur.IsExpanded())
			}
			return nil
		default:
			return event
		}
		return event
	})

	mainGrid := tview.NewGrid().
		SetRows(0, 1).
		SetColumns(-1, -3).
		AddItem(treeGrid, 0, 0, 1, 1, 0, 0, true).
		AddItem(infoPane, 0, 1, 1, 1, 0, 0, false).
		AddItem(statusLine, 1, 0, 1, 2, 0, 0, false)

	tabbableViews := []tview.Primitive{tree, searchInputField, infoPane}
	tabbableViewIdx := 0

	setNewFocusedViewIdx := func(prevIdx int, newIdx int) {
		//		prev := tabbableViews[prevIdx%len(tabbableViews)]
		//		 note: remember to check if prev == new
		new := tabbableViews[newIdx%len(tabbableViews)]
		app.SetFocus(new)
	}

	CurrentViewIsMainGrid := true
	ShowingHelp := false
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch key := event.Key(); key {
		// case tcell.KeyRune:
		// 	if r := event.Rune(); key == tcell.KeyRune && r == 'q' {
		// 		app.Stop()
		// 		return nil
		// 	}
		case tcell.KeyCtrlQ:
			app.Stop()
		case tcell.KeyCtrlH:
			if !ShowingHelp {
				infoPane.SetText(HelpText)
				ShowingHelp = true
			} else {
				treeselfunc(tree.GetCurrentNode())
				ShowingHelp = false
			}
		case tcell.KeyCtrlF:
			// ctrlf is "Full Screen", meaning make copy and paste easy
			if !CurrentViewIsMainGrid {
				infoPane.SetBorder(true)
				app.SetRoot(mainGrid, true)
				CurrentViewIsMainGrid = true
			} else {
				infoPane.SetBorder(false)
				app.SetRoot(infoPane, true)
				CurrentViewIsMainGrid = false
			}
		case tcell.KeyCtrlS:
			prev := tabbableViewIdx
			tabbableViewIdx := 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		case tcell.KeyTab:
			prev := tabbableViewIdx
			tabbableViewIdx += 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		case tcell.KeyBacktab:
			prev := tabbableViewIdx
			tabbableViewIdx -= 1
			setNewFocusedViewIdx(prev, tabbableViewIdx)
		}
		return event
	})

	if err := app.SetRoot(mainGrid, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
	return nil
}
