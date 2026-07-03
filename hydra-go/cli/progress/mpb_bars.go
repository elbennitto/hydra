package progress

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
)

// MpbProgressBars implements [log.ProgressBars] using mpb; logging goes through the mpb container [io.Writer].
type MpbProgressBars struct {
	p            *mpb.Progress
	mu           sync.Mutex
	pWriteMu     sync.Mutex // serializes (*mpb.Progress).Write (slog and stdout pipe forwarder)
	nextPriority int
	bars         []*mpbBar

	// Optional dual-TTY stdout proxy: process writes go to pipeW; forwardStdoutLines reads and sends each line via p.Write.
	origStdout *os.File
	pipeR      *os.File
	pipeW      *os.File
	forwardWG  sync.WaitGroup
}

// stderrTerminalWidth returns the TTY width for stderr, or a safe default when unavailable.
func stderrTerminalWidth() int {
	w, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || w < 20 {
		return 80
	}
	return w
}

// operationNameColumnWidth picks a fixed label column width so the bar filler keeps most of the row
// while leaving room for step counters (append decorators) on the right.
func operationNameColumnWidth(termW int) int {
	// ~30% of terminal for the operation name, clamped; remainder is filler + "n / m".
	w := termW * 3 / 10
	if w < 22 {
		w = 22
	}
	if w > 46 {
		w = 46
	}
	maxLabel := termW - 20 // reserve at least ~20 cols for bar tube + counters
	if maxLabel < 16 {
		maxLabel = 16
	}
	if w > maxLabel {
		w = maxLabel
	}
	return w
}

func coloredBarStyleBuilder() mpb.BarFillerBuilder {
	reset := colors.Reset.String()
	cBrack := colors.LightCyan.String()
	fill := colors.LightBlue.String()
	tip := colors.LightMagenta.String()
	pad := colors.LightGray.String()
	return mpb.BarStyle().
		Lbound("[").LboundMeta(func(s string) string { return cBrack + s + reset }).
		Rbound("]").RboundMeta(func(s string) string { return cBrack + s + reset }).
		Filler("=").FillerMeta(func(s string) string { return fill + s + reset }).
		Refiller("-").RefillerMeta(func(s string) string { return fill + s + reset }).
		Tip(">").TipMeta(func(s string) string { return tip + s + reset }).
		Padding("-").PaddingMeta(func(s string) string { return pad + s + reset })
}

func coloredSpinnerStyleBuilder() mpb.BarFillerBuilder {
	reset := colors.Reset.String()
	spin := colors.LightMagenta.String()
	return mpb.SpinnerStyle().Meta(func(s string) string { return spin + s + reset })
}

// NewMpbProgressBars creates a footer container writing to stderr. Caller must [MpbProgressBars.Close] when done.
func NewMpbProgressBars() *MpbProgressBars {
	tw := stderrTerminalWidth()
	return &MpbProgressBars{
		p: mpb.New(mpb.WithOutput(os.Stderr), mpb.WithWidth(tw), mpb.WithAutoRefresh()),
	}
}

func (m *MpbProgressBars) writeProgress(b []byte) (int, error) {
	m.pWriteMu.Lock()
	defer m.pWriteMu.Unlock()
	if m.p == nil {
		return 0, nil
	}
	return m.p.Write(b)
}

func (m *MpbProgressBars) Write(p []byte) (int, error) {
	return m.writeProgress(p)
}

// InstallStdoutProxyIfNeeded replaces [os.Stdout] with a pipe write end when both stdout and stderr are
// terminals, so user writes to stdout can be forwarded line-by-line through [mpb.Progress.Write] (above the footer).
// It is a no-op if already installed or if either stream is not a TTY.
func (m *MpbProgressBars) InstallStdoutProxyIfNeeded() error {
	if m.origStdout != nil {
		return nil
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	m.origStdout = os.Stdout
	m.pipeR, m.pipeW = r, w
	m.forwardWG.Add(1)
	go m.forwardStdoutLines()
	os.Stdout = w
	return nil
}

// readLines reads r with bufio until EOF, calling emit once per ReadBytes-delimited segment (including
// a trailing line without '\n' before EOF).
func readLines(r io.Reader, emit func([]byte) error) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if emitErr := emit(line); emitErr != nil {
				return emitErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (m *MpbProgressBars) forwardStdoutLines() {
	defer m.forwardWG.Done()
	_ = readLines(m.pipeR, func(line []byte) error {
		_, err := m.writeProgress(line)
		return err
	})
}

func (m *MpbProgressBars) restoreStdoutProxy() {
	if m.origStdout == nil {
		return
	}
	if m.pipeW != nil {
		_ = m.pipeW.Close()
	}
	m.forwardWG.Wait()
	if m.pipeR != nil {
		_ = m.pipeR.Close()
	}
	os.Stdout = m.origStdout
	m.origStdout = nil
	m.pipeR, m.pipeW = nil, nil
}

func (m *MpbProgressBars) Close() error {
	m.restoreStdoutProxy()
	m.mu.Lock()
	toClose := m.bars
	m.bars = nil
	m.mu.Unlock()
	for _, b := range toClose {
		_ = b.Close()
	}
	if m.p != nil {
		m.p.Wait()
		m.p = nil
	}
	return nil
}

func (m *MpbProgressBars) registerBar(b *mpbBar) {
	m.mu.Lock()
	m.bars = append(m.bars, b)
	m.mu.Unlock()
}

func (m *MpbProgressBars) unregisterBar(b *mpbBar) {
	m.mu.Lock()
	for i, x := range m.bars {
		if x == b {
			m.bars = append(m.bars[:i], m.bars[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

// FlushBeforeStdout implements [log.ProgressBars.FlushBeforeStdout].
func (m *MpbProgressBars) FlushBeforeStdout() error {
	m.mu.Lock()
	toClose := m.bars
	m.bars = nil
	m.mu.Unlock()
	for _, b := range toClose {
		_ = b.Close()
	}
	k8s.FlushProgressLogBeforeFooter()
	log.SyncStdoutBestEffort()
	return nil
}

func (m *MpbProgressBars) NewProgress(operation string, total int) (log.Progress, error) {
	k8s.FlushProgressLogBeforeFooter()
	m.mu.Lock()
	pr := m.nextPriority
	m.nextPriority++
	m.mu.Unlock()

	return newMpbBar(m, m.p, operation, total, mpb.BarPriority(pr))
}

type mpbBar struct {
	footer    *MpbProgressBars
	p         *mpb.Progress
	bar       *mpb.Bar
	operation string
	mu        sync.Mutex
	lastIdx   int
	unknown   bool

	taskOrder      []uint64
	taskText       map[uint64]string
	nextTaskID     uint64
	progressClosed atomic.Bool
}

func newMpbBar(footer *MpbProgressBars, p *mpb.Progress, operation string, total int, priority mpb.BarOption) (*mpbBar, error) {
	b := &mpbBar{footer: footer, p: p, taskText: make(map[uint64]string), operation: operation}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastIdx = 0
	b.unknown = total < 0

	tw := stderrTerminalWidth()
	nameW := operationNameColumnWidth(tw)
	opColor := colors.LightGreen.String()
	reset := colors.Reset.String()
	nameDecor := opColor + operation + ": " + reset
	removeOnComplete := true

	if b.unknown {
		opts := []mpb.BarOption{priority}
		if removeOnComplete {
			opts = append(opts, mpb.BarRemoveOnComplete())
		}
		opts = append(opts,
			mpb.PrependDecorators(
				decor.Name(nameDecor, decor.WC{W: nameW, C: decor.DindentRight}),
			),
			mpb.AppendDecorators(
				decor.Any(func(s decor.Statistics) string {
					b.mu.Lock()
					idx := b.lastIdx
					b.mu.Unlock()
					y := colors.LightYellow.String()
					if idx <= 0 {
						return y + " …" + reset
					}
					return fmt.Sprintf("%s #%d%s", y, idx, reset)
				}),
			),
			mpb.BarExtender(mpb.BarFillerFunc(b.detailLineFiller), false),
		)
		b.bar = p.New(1<<20, coloredSpinnerStyleBuilder(), opts...)
		footer.registerBar(b)
		log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} bar opened",
			log.String("operation", operation),
			log.Int("total", total),
		)
		return b, nil
	}

	t := int64(total)
	if t < 1 {
		t = 1
	}
	y := colors.LightYellow.String()
	opts := []mpb.BarOption{priority}
	if removeOnComplete {
		opts = append(opts, mpb.BarRemoveOnComplete())
	}
	opts = append(opts,
		mpb.PrependDecorators(
			decor.Name(nameDecor, decor.WC{W: nameW, C: decor.DindentRight}),
		),
		mpb.AppendDecorators(
			decor.Any(func(s decor.Statistics) string {
				return fmt.Sprintf("%s%d / %d%s", y, s.Current, s.Total, reset)
			}),
		),
		mpb.BarExtender(mpb.BarFillerFunc(b.detailLineFiller), false),
	)
	b.bar = p.New(t, coloredBarStyleBuilder(), opts...)
	footer.registerBar(b)
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} bar opened",
		log.String("operation", operation),
		log.Int("total", total),
	)
	return b, nil
}

// detailLineFiller writes one line per active [log.ProgressTask] below the bar.
func (b *mpbBar) detailLineFiller(w io.Writer, _ decor.Statistics) error {
	b.mu.Lock()
	lines := make([]string, len(b.taskOrder))
	for i, id := range b.taskOrder {
		t := b.taskText[id]
		if t == "" {
			t = " "
		}
		lines[i] = t
	}
	b.mu.Unlock()

	dim := colors.LightGray.String()
	reset := colors.Reset.String()
	var err error
	for _, line := range lines {
		_, e := fmt.Fprintf(w, "%s%s%s\n", dim, line, reset)
		if e != nil {
			err = e
		}
	}
	return err
}

func (m *mpbBar) Advance(index int, total int) {
	m.mu.Lock()
	if index > 0 {
		m.lastIdx = index
	}
	op := m.operation
	m.mu.Unlock()

	if m.bar == nil {
		return
	}
	if m.unknown {
		m.bar.Increment()
		log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} bar advance",
			log.String("operation", op),
			log.Int("index", index),
			log.Int("total", total),
		)
		return
	}
	if total > 0 {
		m.bar.SetTotal(int64(total), false)
	}
	if total > 0 && index >= 1 {
		m.bar.SetCurrent(int64(index))
	} else {
		m.bar.Increment()
	}
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} bar advance",
		log.String("operation", op),
		log.Int("index", index),
		log.Int("total", total),
	)
}

func (m *mpbBar) NewTask(name string) log.ProgressTask {
	m.mu.Lock()
	if m.progressClosed.Load() || m.bar == nil {
		m.mu.Unlock()
		return noopProgressTask{}
	}
	id := m.nextTaskID
	m.nextTaskID++
	m.taskOrder = append(m.taskOrder, id)
	m.taskText[id] = name
	op := m.operation
	m.mu.Unlock()
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} status opened",
		log.String("operation", op),
		log.String("name", name),
		log.Int("taskId", int(id)),
	)
	return &mpbTask{parent: m, id: id}
}

func (m *mpbBar) clearAllTasksLocked() {
	m.taskOrder = m.taskOrder[:0]
	for id := range m.taskText {
		delete(m.taskText, id)
	}
}

func (m *mpbBar) removeTask(id uint64) {
	m.mu.Lock()
	if _, ok := m.taskText[id]; !ok {
		m.mu.Unlock()
		return
	}
	delete(m.taskText, id)
	for i, tid := range m.taskOrder {
		if tid == id {
			m.taskOrder = append(m.taskOrder[:i], m.taskOrder[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

func (m *mpbBar) setTaskDetail(id uint64, detail string) {
	m.mu.Lock()
	if _, ok := m.taskText[id]; !ok {
		m.mu.Unlock()
		return
	}
	m.taskText[id] = detail
	op := m.operation
	m.mu.Unlock()
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} status updated",
		log.String("operation", op),
		log.Int("taskId", int(id)),
		log.String("detail", detail),
	)
}

func (m *mpbBar) Close() error {
	if !m.progressClosed.CompareAndSwap(false, true) {
		return nil
	}
	m.mu.Lock()
	m.clearAllTasksLocked()
	bar := m.bar
	m.bar = nil
	op := m.operation
	m.mu.Unlock()
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} bar closed",
		log.String("operation", op),
	)
	if m.footer != nil {
		m.footer.unregisterBar(m)
		m.footer = nil
	}
	if bar != nil {
		bar.Abort(true)
	}
	return nil
}

// mpbTask is one status line under an [mpbBar].
type mpbTask struct {
	parent *mpbBar
	id     uint64
	closed atomic.Bool
}

func (t *mpbTask) SetDetail(detail string) {
	if t.closed.Load() || t.parent == nil {
		return
	}
	t.parent.setTaskDetail(t.id, detail)
}

func (t *mpbTask) Close() error {
	if t.parent == nil {
		return nil
	}
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	parent := t.parent
	id := t.id
	parent.removeTask(t.id)
	log.Default().DebugLog(log.Hydra().Child("progress").Child("mpb"), "{operation} status closed",
		log.String("operation", parent.operation),
		log.Int("taskId", int(id)),
	)
	return nil
}

// noopProgressTask is returned when the parent bar is already closed or unavailable.
type noopProgressTask struct{}

func (noopProgressTask) SetDetail(string) {}
func (noopProgressTask) Close() error     { return nil }
