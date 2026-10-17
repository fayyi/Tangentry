"""Tangentry's local CLI adapter, using faster-whisper only as infrastructure."""
import json
import math
import sys
import wave
import numpy as np
from pathlib import Path
from faster_whisper import WhisperModel

def is_near_digital_silence(samples):
    # Whisper commonly hallucinates over pure silence. Keep quiet speech and
    # music on the normal recognition path; this threshold is about -70 dBFS.
    return not samples.size or float(np.max(np.abs(samples))) <= 10 / 32768


def subtitle_parts(segment):
    words = getattr(segment, "words", None)
    if not words or "".join(word.word for word in words).split() != segment.text.split():
        return [(segment.start, segment.end, segment.text)]
    previous = segment.start
    for word in words:
        if (not math.isfinite(word.start) or not math.isfinite(word.end)
                or word.start < previous or word.end < word.start
                or word.start < segment.start or word.end > segment.end + .001):
            return [(segment.start, segment.end, segment.text)]
        previous = word.start
    # Boundaries come from aligned words, never from distributing time by text length.
    parts = []
    start, end, text = words[0].start, words[0].end, ""
    for word in words:
        if not text:
            start = word.start
        if text and end > start and (word.start - end >= .8
                or word.end - start > 8 or len(text + word.word) > 48):
            parts.append((start, end, text))
            start, text = word.start, ""
        text += word.word
        end = word.end
        if end > start and text.rstrip().endswith(("。", "！", "？", ".", "!", "?")):
            parts.append((start, end, text))
            start, text = end, ""
    if text:
        if end <= start:
            return [(segment.start, segment.end, segment.text)]
        parts.append((start, end, text))
    return parts


model_path, audio, language, output = sys.argv[1:]
# A directory is mandatory. Never resolve a model name or download during jobs.
if not Path(model_path).is_dir():
    raise SystemExit(1)
# FFmpeg already normalized the source. Passing samples directly avoids a
# redundant decoder and PyAV's removed metadata_errors API.
with wave.open(audio, "rb") as source:
    if source.getnchannels() != 1 or source.getsampwidth() != 2 or source.getframerate() != 16000 or source.getnframes() > 6 * 3600 * 16000:
        raise SystemExit(1)
    samples = np.frombuffer(source.readframes(source.getnframes()), dtype="<i2").astype(np.float32) / 32768.0
    duration_ms = round(source.getnframes() * 1000 / source.getframerate())
if is_near_digital_silence(samples):
    raise SystemExit("no speech")
model = WhisperModel(model_path, device="cpu", compute_type="int8", cpu_threads=4, num_workers=1, local_files_only=True)
segments, info = model.transcribe(samples, language=None if language == "auto" else language, beam_size=5, vad_filter=False, word_timestamps=True)
result = []
for segment in segments:
    # Whisper can timestamp padded silence beyond the last real sample. Keep
    # generated cues within the normalized audio, including 60-second previews.
    for part_start, part_end, text in subtitle_parts(segment):
        start = round(part_start * 1000)
        end = min(duration_ms, round(part_end * 1000))
        if start >= duration_ms or end <= start:
            continue
        if len(result) >= 10000:
            raise SystemExit(1)
        result.append({"offsets": {"from": start, "to": end}, "text": text})
Path(output).write_text(json.dumps({"result": {"language": info.language}, "transcription": result}), encoding="utf-8")
