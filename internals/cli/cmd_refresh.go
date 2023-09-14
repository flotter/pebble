// Copyright (c) 2022 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/canonical/go-flags"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/progress"
)

var (
	reconnectTimeout = 5 * time.Second
	progressPollTime = 100 * time.Millisecond
)

const cmdRefreshSummary = "Update device firmware"
const cmdRefreshDescription = `
Update device with supplied firmware. Restart required to take effect.
`

type cmdRefresh struct {
	client *client.Client

	Positional struct {
		LocalPath string `positional-arg-name:"<local-path>" required:"1"`
	} `positional-args:"yes"`
}

func init() {
	AddCommand(&CmdInfo{
		Name:        "refresh",
		Summary:     cmdRefreshSummary,
		Description: cmdRefreshDescription,
		New:         func(opts *CmdOptions) flags.Commander {
			return &cmdRefresh{
				client: opts.Client,
			}
		},
	})
}
func (cmd *cmdRefresh) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	f, err := os.Open(cmd.Positional.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	asyncError := make(chan error)
	defer func() {
		close(asyncError)
	}()

	changeId, err := cmd.client.Refresh(&client.RefreshOptions{
		Firmware: &client.FwFile{
			Source:      f,
			Size:        st.Size(),
			UploadError: asyncError,
		},
	})
	if err != nil {
		return err
	}

	// Deal with user cancellation
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt)
	go func() {
		select {
		// We received a signal or channel closed
		case sig := <-sigs:
			if sig == nil {
				return
			}
			// Cancel refresh
			_, err := cmd.client.Abort(changeId)
			if err != nil {
				fmt.Fprintf(Stderr, err.Error()+"\n")
			}
		// Upload was interrupted or channel closed
		case err := <-asyncError:
			if err == nil {
				return
			}
		}
	}()

	pb := progress.MakeProgressBar()
	defer func() {
		pb.Finished()
		// Next two are not strictly needed for CLI, but
		// without them the tests will leak goroutines.
		signal.Stop(sigs)
		close(sigs)
	}()

	tMax := time.Time{}

	var lastID string
	lastLog := map[string]string{}
	for {
		var rebootingErr error
		chg, err := cmd.client.Change(changeId)
		if err != nil {
			// A client.Error means we were able to communicate with
			// the server (got an answer).
			if e, ok := err.(*client.Error); ok {
				return e
			}

			// A non-client error here means the server most
			// likely went away
			// XXX: it actually can be a bunch of other things; fix client to expose it better
			now := time.Now()
			if tMax.IsZero() {
				tMax = now.Add(reconnectTimeout)
			}
			if now.After(tMax) {
				return err
			}
			pb.Spin("Waiting for server to restart")
			time.Sleep(progressPollTime)
			continue
		}
		if maintErr, ok := cmd.client.Maintenance().(*client.Error); ok && maintErr.Kind == client.ErrorKindSystemRestart {
			rebootingErr = maintErr
		}
		if !tMax.IsZero() {
			pb.Finished()
			tMax = time.Time{}
		}

		for _, t := range chg.Tasks {
			switch {
			case t.Status != "Doing":
				continue
			case t.Progress.Total == 1:
				pb.Spin(t.Summary)
				nowLog := lastLogStr(t.Log)
				if lastLog[t.ID] != nowLog {
					pb.Notify(nowLog)
					lastLog[t.ID] = nowLog
				}
			case t.ID == lastID:
				pb.Set(float64(t.Progress.Done))
			default:
				pb.Start(t.Summary, float64(t.Progress.Total))
				lastID = t.ID
			}
			break
		}

		if chg.Ready {
			if chg.Status == "Done" {
				return nil
			}

			if chg.Err != "" {
				return errors.New(chg.Err)
			}

			return fmt.Errorf("change finished in status %q with no error message", chg.Status)
		}

		if rebootingErr != nil {
			return rebootingErr
		}

		// Not a ticker so it sleeps 100ms between calls
		// rather than call once every 100ms.
		time.Sleep(progressPollTime)
	}

	return nil
}
