package main

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---------------------------------------------------------------------------
// Big round sync button
// ---------------------------------------------------------------------------

type syncState int

const (
	stateIdle    syncState = iota // ready: dark disc, "Synchroniser"
	stateRunning                  // progress ring, "2/3"
	stateDone                     // green disc, "À jour"
	stateError                    // red disc, "Réessayer"
)

// syncButton is a large circular, tappable widget whose look follows the
// current sync state.
type syncButton struct {
	widget.BaseWidget
	state    syncState
	progress float64 // 0..1, only used while running
	title    string
	sub      string
	onTap    func()
}

func newSyncButton(onTap func()) *syncButton {
	b := &syncButton{state: stateIdle, title: "Synchroniser", onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// Set updates the displayed state. Must be called on the UI thread.
func (b *syncButton) Set(state syncState, progress float64, title, sub string) {
	b.state = state
	b.progress = progress
	b.title = title
	b.sub = sub
	b.Refresh()
}

// Tapped implements fyne.Tappable.
func (b *syncButton) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *syncButton) CreateRenderer() fyne.WidgetRenderer {
	r := &syncRenderer{b: b}
	r.disc = canvas.NewRaster(r.draw)
	r.title = canvas.NewText("", color.White)
	r.title.Alignment = fyne.TextAlignCenter
	r.title.TextStyle = fyne.TextStyle{Bold: true}
	r.sub = canvas.NewText("", color.White)
	r.sub.Alignment = fyne.TextAlignCenter
	return r
}

type syncRenderer struct {
	b     *syncButton
	disc  *canvas.Raster
	title *canvas.Text
	sub   *canvas.Text
}

func (r *syncRenderer) MinSize() fyne.Size { return fyne.NewSize(160, 160) }

func (r *syncRenderer) Layout(size fyne.Size) {
	r.disc.Resize(size)
	r.disc.Move(fyne.NewPos(0, 0))
	r.place(size)
}

// place sizes and centers the texts according to the current size.
func (r *syncRenderer) place(size fyne.Size) {
	r.title.TextSize = size.Height * 0.13
	r.sub.TextSize = size.Height * 0.075
	titleH := r.title.MinSize().Height
	subH := float32(0)
	if r.b.sub != "" {
		subH = r.sub.MinSize().Height
	}
	total := titleH + subH
	top := (size.Height - total) / 2
	r.title.Resize(fyne.NewSize(size.Width, titleH))
	r.title.Move(fyne.NewPos(0, top))
	r.sub.Resize(fyne.NewSize(size.Width, subH))
	r.sub.Move(fyne.NewPos(0, top+titleH))
}

func (r *syncRenderer) Refresh() {
	fg := theme.Color(theme.ColorNameForeground)
	onFill := color.Color(color.White)
	switch r.b.state {
	case stateIdle:
		onFill = theme.Color(theme.ColorNameBackground)
	case stateRunning:
		onFill = fg
	}
	r.title.Text = r.b.title
	r.title.Color = onFill
	r.sub.Text = r.b.sub
	r.sub.Color = onFill
	r.place(r.b.Size())
	r.disc.Refresh()
	r.title.Refresh()
	r.sub.Refresh()
}

func (r *syncRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.disc, r.title, r.sub}
}

func (r *syncRenderer) Destroy() {}

// draw paints the disc (or progress ring) with simple edge anti-aliasing.
func (r *syncRenderer) draw(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}
	cx, cy := float64(w)/2, float64(h)/2
	rad := math.Min(cx, cy) - 2
	if rad <= 0 {
		return img
	}

	var fill color.Color
	ring := r.b.state == stateRunning
	switch r.b.state {
	case stateIdle:
		fill = theme.Color(theme.ColorNameForeground)
	case stateDone:
		fill = theme.Color(theme.ColorNameSuccess)
	case stateError:
		fill = theme.Color(theme.ColorNameError)
	}
	track := theme.Color(theme.ColorNameInputBorder)
	prog := theme.Color(theme.ColorNamePrimary)
	thick := rad * 0.14

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Hypot(dx, dy)
			if d > rad+1 {
				continue
			}
			var c color.Color
			var cov float64
			if ring {
				cov = math.Min(clamp01(rad-d+0.5), clamp01(d-(rad-thick)+0.5))
				// clockwise from 12 o'clock
				a := math.Atan2(dx, -dy)
				if a < 0 {
					a += 2 * math.Pi
				}
				if a/(2*math.Pi) <= r.b.progress {
					c = prog
				} else {
					c = track
				}
			} else {
				cov = clamp01(rad - d + 0.5)
				c = fill
			}
			if cov <= 0 {
				continue
			}
			cr, cg, cb, ca := c.RGBA() // premultiplied, 16-bit
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(float64(cr>>8) * cov),
				G: uint8(float64(cg>>8) * cov),
				B: uint8(float64(cb>>8) * cov),
				A: uint8(float64(ca>>8) * cov),
			})
		}
	}
	return img
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------------------
// Layouts
// ---------------------------------------------------------------------------

// squareLayout keeps its single child a centered square that scales with the
// available space (between 160 and 300 px).
type squareLayout struct{}

func (squareLayout) MinSize(_ []fyne.CanvasObject) fyne.Size { return fyne.NewSize(180, 200) }

func (squareLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	d := fyne.Min(size.Width*0.85, size.Height*0.9)
	if d > 300 {
		d = 300
	}
	if d < 160 {
		d = 160
	}
	for _, o := range objs {
		o.Resize(fyne.NewSize(d, d))
		o.Move(fyne.NewPos((size.Width-d)/2, (size.Height-d)/2))
	}
}

// maxWidthLayout centers its child and limits its width (for forms on wide
// screens).
type maxWidthLayout struct{ max float32 }

func (l maxWidthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objs[0].MinSize()
}

func (l maxWidthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	w := fyne.Min(size.Width, l.max)
	objs[0].Resize(fyne.NewSize(w, size.Height))
	objs[0].Move(fyne.NewPos((size.Width-w)/2, 0))
}

// adaptiveLayout arranges three children: main, toggle, details.
//
//   - narrow window: one column — main (fills the free height), then the
//     toggle button, then details when visible;
//   - wide window (>= wideBreakpoint): two columns — main on the left,
//     details always shown on the right, toggle hidden.
//
// onMode is called (once) whenever the layout switches between the modes.
type adaptiveLayout struct {
	wide   bool
	onMode func(wide bool)
}

const wideBreakpoint = 720

func (l *adaptiveLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) < 3 {
		return fyne.NewSize(0, 0)
	}
	main, toggle, details := objs[0].MinSize(), objs[1].MinSize(), objs[2].MinSize()
	h := main.Height + toggle.Height
	if objs[2].Visible() {
		h += details.Height
	}
	return fyne.NewSize(fyne.Max(main.Width, fyne.Max(toggle.Width, details.Width)), h)
}

func (l *adaptiveLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 3 {
		return
	}
	main, toggle, details := objs[0], objs[1], objs[2]
	wide := size.Width >= wideBreakpoint
	if wide != l.wide {
		l.wide = wide
		if l.onMode != nil {
			l.onMode(wide)
		}
	}

	if wide {
		left := fyne.Max(size.Width*0.42, main.MinSize().Width)
		main.Resize(fyne.NewSize(left, size.Height))
		main.Move(fyne.NewPos(0, 0))
		details.Resize(fyne.NewSize(size.Width-left, size.Height))
		details.Move(fyne.NewPos(left, 0))
		toggle.Resize(fyne.NewSize(0, 0))
		return
	}

	toggleH := toggle.MinSize().Height
	detailsH := float32(0)
	if details.Visible() {
		detailsH = details.MinSize().Height
	}
	mainH := fyne.Max(main.MinSize().Height, size.Height-toggleH-detailsH)
	main.Resize(fyne.NewSize(size.Width, mainH))
	main.Move(fyne.NewPos(0, 0))
	toggle.Resize(fyne.NewSize(size.Width, toggleH))
	toggle.Move(fyne.NewPos(0, mainH))
	details.Resize(fyne.NewSize(size.Width, detailsH))
	details.Move(fyne.NewPos(0, mainH+toggleH))
}
