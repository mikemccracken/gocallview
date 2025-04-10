# TODO notes

## bugs:



## big use case

I have an error message, I find the string, I want to see paths that might get me there.

## misc

- want a way to get the paths as urls that work when shared

- want initial display to not show stdlib calls, with a dynamic expansion option to do so 
- would like better func Name strings - even more than func.String, which is better than func.Name

- want search-from-current node because it has to expand fully and might be slow.
- want nSites for total paths and also for displayed/expanded paths.

want to cycle to focus on a node, show all paths that get to it in one place

- search then expand a node needs thinking through 


want to show control flow that dominates a callsite, e.g. when we say that
OpenAtomix calls UpgradeBootMgr, but it only does so if three things are true:
then we should have those things in the note:
and maybe we can filter results that do not depend on these? or just squelch results as we see ifs that don't apply for us right now?

 342     if !opts.NoBootManager && !bootMgrLogged && !opts.ManifestReadOnly {
 343         // upgrade from pre-bootmgr
 344         log.Infof("Upgrading from pre-bootmgr system to bootmgr")
 345         a.bootmgr, err = bootmgr.UpgradeBootMgr(a.rootfs)

