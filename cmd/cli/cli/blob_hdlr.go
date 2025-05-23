// Package cli provides easy-to-use commands to manage, monitor, and utilize AIS clusters.
// This file handles commands that control running jobs in the cluster.
/*
 * Copyright (c) 2024, NVIDIA CORPORATION. All rights reserved.
 */
package cli

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/aistore/api"
	"github.com/NVIDIA/aistore/api/apc"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/debug"
	"github.com/NVIDIA/aistore/xact"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v4"
)

func blobDownloadHandler(c *cli.Context) error {
	var (
		objNames []string
		bck      cmn.Bck
		err      error
		uri      = c.Args().Get(0)
	)
	if flagIsSet(c, listFlag) {
		listObjs := parseStrFlag(c, listFlag)
		objNames = splitCsv(listObjs)
		bck, err = parseBckURI(c, uri, true)
	} else {
		var objName string
		bck, objName, err = parseBckObjURI(c, uri, false /*emptyObjnameOK*/)
		objNames = []string{objName}
	}
	if err != nil {
		return err
	}
	if bck.Props, err = headBucket(bck, true /* add */); err != nil {
		return err
	}
	if !bck.IsRemote() {
		return fmt.Errorf("expecting remote bucket (have %s)", bck.Cname(""))
	}

	// message
	var msg apc.BlobMsg
	if flagIsSet(c, chunkSizeFlag) {
		msg.ChunkSize, err = parseSizeFlag(c, chunkSizeFlag)
		if err != nil {
			return err
		}
	}
	if flagIsSet(c, numBlobWorkersFlag) {
		msg.NumWorkers = parseIntFlag(c, numBlobWorkersFlag)
	}
	msg.LatestVer = flagIsSet(c, latestVerFlag)

	// start
	var (
		xids    []string
		cnt     int
		timeout time.Duration
	)
	xids, cnt, err = blobStartAll(c, bck, objNames, &msg)
	if err != nil || cnt == 0 /* nothing to do */ {
		return err
	}
	if flagIsSet(c, waitJobXactFinishedFlag) {
		timeout = parseDurationFlag(c, waitJobXactFinishedFlag)
	}

	// show progress, wait, or simply return
	switch {
	case flagIsSet(c, progressFlag):
		err = blobAllProgress(c, bck, objNames, xids)
	case len(objNames) == 1:
		xid, objName := xids[0], objNames[0]
		cname := xact.Cname(apc.ActBlobDl, xid)
		text := cname + " " + bck.Cname(objName)
		if !flagIsSet(c, waitFlag) && timeout == 0 {
			if flagIsSet(c, nonverboseFlag) {
				fmt.Fprintln(c.App.Writer, xid)
			} else {
				actionDone(c, text+". "+toMonitorMsg(c, xid, ""))
			}
			return nil
		}
		xargs := xact.ArgsMsg{ID: xid, Kind: apc.ActBlobDl, Timeout: timeout}
		err = _blobWaitOne(c, &xargs, text)
	default:
		if !flagIsSet(c, waitFlag) && timeout == 0 {
			if flagIsSet(c, nonverboseFlag) {
				fmt.Fprintln(c.App.Writer, strings.Join(xids, " "))
			} else {
				text := fmt.Sprintf("%s[%v]", apc.ActBlobDl, strings.Join(xids, ", "))
				actionDone(c, text+". "+toMonitorMsg(c, apc.ActBlobDl, ""))
			}
			return nil
		}
		// wait multiple (no progress)
		var (
			wg    = &sync.WaitGroup{}
			errCh = make(chan error, len(objNames))
		)
		wg.Add(len(objNames))
		for i := range objNames {
			objName, xid := objNames[i], xids[i]
			if xid == "" {
				continue // nothing to do
			}
			cname := xact.Cname(apc.ActBlobDl, xid)
			fmt.Fprintln(c.App.Writer, fcyan(cname))
			go func(objName, xid, cname string, timeout time.Duration) {
				text := cname + " " + bck.Cname(objName)
				xargs := xact.ArgsMsg{ID: xid, Kind: apc.ActBlobDl, Timeout: timeout}
				err := _blobWaitOne(c, &xargs, text)
				if err != nil {
					errCh <- err
				}
				wg.Done()
			}(objName, xid, cname, timeout)
		}
		wg.Wait()
		select {
		case err = <-errCh:
		default:
		}
		close(errCh)
	}
	if err == nil {
		actionDone(c, fmtXactSucceeded)
	}
	return err
}

func blobStartAll(c *cli.Context, bck cmn.Bck, objNames []string, msg *apc.BlobMsg) (xids []string, cnt int, err error) {
	var xid string
	xids = make([]string, 0, len(objNames))
	for _, objName := range objNames {
		xid, err = api.BlobDownload(apiBP, bck, objName, msg)
		if err != nil {
			for _, xid = range xids {
				errN := xstop(&xact.ArgsMsg{ID: xid, Kind: apc.ActBlobDl})
				if errN != nil {
					actionWarn(c, errN.Error())
				}
			}
			return nil, 0, V(err)
		}
		if xid == "" {
			actionDone(c, bck.Cname(objName)+" already downloaded, nothing to do")
		} else {
			cnt++
		}
		xids = append(xids, xid)
	}
	return xids, cnt, nil
}

func blobAllProgress(c *cli.Context, bck cmn.Bck, objNames, xids []string) (err error) {
	var (
		bargs       = make([]barArgs, 0, len(xids))
		errCh       = make(chan error, len(xids))
		refreshRate = _refreshRate(c)
	)
	debug.Assert(len(xids) == len(objNames)) // xid <=1:1=> objName
	for i := range xids {
		if xids[i] != "" {
			bargs = append(bargs, barArgs{barType: sizeArg, barText: bck.Cname(objNames[i]), total: 0})
		}
	}
	progress, bars := simpleBar(bargs...)
	for i := range objNames {
		if xids[i] != "" {
			xid, bar := xids[i], bars[i]
			cname := xact.Cname(apc.ActBlobDl, xid)
			fmt.Fprintln(c.App.Writer, fcyan(cname))
			go _blobOneProgress(xid, bar, errCh, refreshRate)
		}
	}
	progress.Wait()
	select {
	case err = <-errCh:
	default:
	}
	if len(objNames) == 1 {
		close(errCh)
	}
	return err
}

func _blobOneProgress(xid string, bar *mpb.Bar, errCh chan error, sleep time.Duration) {
	var (
		xargs    = xact.ArgsMsg{ID: xid, Kind: apc.ActBlobDl}
		currSize int64
		fullSize = int64(-1)
		done     bool
	)
	for {
		daemonID, snap, errN := getAnyXactSnap(&xargs)
		if errN != nil {
			errCh <- errN
			break
		}
		done = snap.Finished()
		debug.Assert(snap.ID == xargs.ID)
		if xargs.DaemonID == "" {
			xargs.DaemonID = daemonID
		}
		if fullSize < 0 && snap.Stats.InBytes != 0 {
			fullSize = snap.Stats.InBytes
			bar.SetTotal(fullSize, false)
		}
		debug.Assert(xargs.DaemonID == daemonID)
		if snap.Stats.Bytes != 0 {
			bar.IncrInt64(snap.Stats.Bytes - currSize)
			currSize = snap.Stats.Bytes
			if currSize == fullSize {
				done = true
			}
		}
		if snap.IsAborted() && (fullSize > 0 && currSize < fullSize) {
			errCh <- errors.New(snap.AbortErr)
			break
		}
		if done {
			bar.SetTotal(currSize, true)
			return // --> ok
		}
		time.Sleep(sleep)
	}
	bar.Abort(true)
}

func _blobWaitOne(c *cli.Context, xargs *xact.ArgsMsg, text string) error {
	fmt.Fprintln(c.App.Writer, text+" ...")
	return waitXactBlob(xargs)
}
