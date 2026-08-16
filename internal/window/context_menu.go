package window

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"github.com/JediWattson/gossamer/internal/browser"
)

type DownloadRequest struct {
	URL           *url.URL
	SuggestedName string
}

// DownloadHandler starts a browser-owned download. The shell supplies only a
// validated HTTP(S) URL and suggested name; filesystem policy belongs to the
// embedding launcher.
type DownloadHandler func(context.Context, DownloadRequest) error

func (shell *graphiteShell) handleContextMenu(ctx context.Context, page *browser.Page, backend Backend, event Event) (bool, error) {
	menuBackend, ok := backend.(ContextMenuBackend)
	if !ok || shell == nil || page == nil || event.Kind != EventPointerDown || event.Button != 1 {
		return false, nil
	}
	layout := shell.layout()
	pointX := event.X - float64(layout.content.Min.X)
	pointY := event.Y - float64(layout.content.Min.Y)
	if pointX < 0 || pointY < 0 || pointX >= float64(layout.content.Dx()) || pointY >= float64(layout.content.Dy()) {
		return false, nil
	}
	var target browser.ContextTarget
	if handle, hit := page.HitTest(pointX, pointY); hit {
		target, _ = page.ContextTarget(handle)
	}
	items := []ContextMenuItem{
		{Action: ContextActionBack, Label: "Back", Enabled: page.CanGoBack()},
		{Action: ContextActionForward, Label: "Forward", Enabled: page.CanGoForward()},
		{Action: ContextActionReload, Label: "Reload", Enabled: page.URL() != nil},
	}
	if target.LinkURL != nil {
		items = append(items,
			ContextMenuItem{Action: ContextActionOpenLink, Label: "Open Link in New Tab", Enabled: shell.opener != nil && len(shell.tabs) < maximumGraphiteTabs},
			ContextMenuItem{Action: ContextActionCopyLink, Label: "Copy Link", Enabled: true},
		)
	}
	downloadURL := target.LinkURL
	if downloadURL == nil {
		downloadURL = target.ImageURL
	}
	if downloadURL != nil && shell.downloader != nil {
		items = append(items, ContextMenuItem{Action: ContextActionDownload, Label: "Download", Enabled: true})
	}
	action, err := menuBackend.ShowContextMenu(ContextMenu{X: event.X, Y: event.Y, Items: items})
	if err != nil {
		return true, err
	}
	switch action {
	case ContextActionNone:
		return true, nil
	case ContextActionBack:
		return true, shell.navigateHistory(ctx, page, -1)
	case ContextActionForward:
		return true, shell.navigateHistory(ctx, page, 1)
	case ContextActionReload:
		return true, shell.reload(ctx, page)
	case ContextActionOpenLink:
		if target.LinkURL == nil {
			return true, nil
		}
		if err := shell.openTab(ctx); err != nil {
			return true, err
		}
		return true, shell.navigate(ctx, shell.activePage(), target.LinkURL.String())
	case ContextActionCopyLink:
		clipboard, ok := backend.(ClipboardBackend)
		if !ok || target.LinkURL == nil {
			return true, nil
		}
		return true, clipboard.WriteClipboardText(target.LinkURL.String())
	case ContextActionDownload:
		if shell.downloader == nil || downloadURL == nil {
			return true, nil
		}
		name := path.Base(downloadURL.Path)
		if name == "." || name == "/" || name == "" {
			name = "download"
		}
		return true, shell.downloader(ctx, DownloadRequest{URL: downloadURL, SuggestedName: name})
	default:
		return true, fmt.Errorf("window: unsupported context action %q", action)
	}
}
