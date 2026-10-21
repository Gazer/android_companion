//go:build cgo && !nomirror

package video

/*
#cgo pkg-config: libavcodec libavutil libswscale
#include <errno.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavutil/error.h>
#include <libavutil/log.h>
#include <libswscale/swscale.h>

typedef struct {
	AVCodecContext *ctx;
	AVPacket *pkt;
	AVFrame *frame;
	struct SwsContext *sws;
} decoder;

static void decoder_free(decoder *d) {
	if (!d) return;
	sws_freeContext(d->sws);
	av_frame_free(&d->frame);
	av_packet_free(&d->pkt);
	avcodec_free_context(&d->ctx);
	av_free(d);
}

static int decoder_new(decoder **out) {
	av_log_set_level(AV_LOG_ERROR); // FFmpeg logs to stderr by default
	const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_H264);
	if (!codec) return AVERROR_DECODER_NOT_FOUND;
	decoder *d = av_mallocz(sizeof(decoder));
	if (!d) return AVERROR(ENOMEM);
	d->ctx = avcodec_alloc_context3(codec);
	d->pkt = av_packet_alloc();
	d->frame = av_frame_alloc();
	if (!d->ctx || !d->pkt || !d->frame) {
		decoder_free(d);
		return AVERROR(ENOMEM);
	}
	// Low latency: output frames as soon as they're decoded, and only
	// use slice threads (frame threads delay the output by one frame
	// per thread).
	d->ctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
	d->ctx->thread_type = FF_THREAD_SLICE;
	int ret = avcodec_open2(d->ctx, codec, NULL);
	if (ret < 0) {
		decoder_free(d);
		return ret;
	}
	*out = d;
	return 0;
}

// decoder_send copies the packet (the decoder needs padding after it).
static int decoder_send(decoder *d, const uint8_t *data, int size) {
	int ret = av_new_packet(d->pkt, size);
	if (ret < 0) return ret;
	memcpy(d->pkt->data, data, size);
	ret = avcodec_send_packet(d->ctx, d->pkt);
	av_packet_unref(d->pkt);
	return ret;
}

// decoder_receive returns 0 with the frame size when a frame is ready,
// AVERROR(EAGAIN) when the decoder needs more input.
static int decoder_receive(decoder *d, int *w, int *h) {
	int ret = avcodec_receive_frame(d->ctx, d->frame);
	if (ret == 0) {
		*w = d->frame->width;
		*h = d->frame->height;
	}
	return ret;
}

// decoder_convert writes the last received frame as RGBA.
static int decoder_convert(decoder *d, uint8_t *dst, int stride) {
	AVFrame *f = d->frame;
	d->sws = sws_getCachedContext(d->sws, f->width, f->height, f->format,
		f->width, f->height, AV_PIX_FMT_RGBA, SWS_POINT, NULL, NULL, NULL);
	if (!d->sws) return AVERROR(EINVAL);
	uint8_t *dsts[4] = {dst, NULL, NULL, NULL};
	int strides[4] = {stride, 0, 0, 0};
	sws_scale(d->sws, (const uint8_t *const *)f->data, f->linesize, 0, f->height, dsts, strides);
	return 0;
}

static int is_eagain(int err) { return err == AVERROR(EAGAIN); }

static void error_string(int err, char *buf, size_t size) { av_strerror(err, buf, size); }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Available reports whether this build can decode video.
const Available = true

// Decoder decodes an H.264 Annex-B stream (one packet per frame, as sent by
// scrcpy) into RGBA images, with FFmpeg's libavcodec.
type Decoder struct {
	d *C.decoder
}

// NewH264 creates a decoder. Close it when done.
func NewH264() (*Decoder, error) {
	var d *C.decoder
	if ret := C.decoder_new(&d); ret < 0 {
		return nil, fmt.Errorf("h264 decoder: %w", avError(ret))
	}
	return &Decoder{d: d}, nil
}

// Decode feeds one packet. Every frame it produces is converted into an
// image from sink, then published.
func (d *Decoder) Decode(data []byte, sink Sink) error {
	if len(data) == 0 {
		return nil
	}
	if ret := C.decoder_send(d.d, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.int(len(data))); ret < 0 {
		return fmt.Errorf("decode: %w", avError(ret))
	}
	for {
		var w, h C.int
		ret := C.decoder_receive(d.d, &w, &h)
		if C.is_eagain(ret) != 0 {
			return nil
		}
		if ret < 0 {
			return fmt.Errorf("decode: %w", avError(ret))
		}
		img := sink.Buffer(int(w), int(h))
		if img == nil {
			continue // the sink drops this frame
		}
		if ret := C.decoder_convert(d.d, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Stride)); ret < 0 {
			return fmt.Errorf("convert: %w", avError(ret))
		}
		sink.Publish(img)
	}
}

// Close releases the decoder.
func (d *Decoder) Close() {
	C.decoder_free(d.d)
	d.d = nil
}

type avErr struct{ msg string }

func (e avErr) Error() string { return e.msg }

func avError(code C.int) error {
	var buf [128]C.char
	C.error_string(code, &buf[0], C.size_t(len(buf)))
	return avErr{msg: C.GoString(&buf[0])}
}
