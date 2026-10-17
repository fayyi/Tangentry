"""Offline Qwen3-ASR adapter. All boundaries come from the local forced aligner."""
import json
import math
import os
import sys
import unicodedata
import wave
from array import array
from pathlib import Path

# Both model and processor loading must remain offline, including nested loaders.
os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"
os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"


def audio_blocks(source):
    rate = source.getframerate()
    maximum = 30 * rate
    window = rate // 10
    while True:
        offset = source.tell()
        raw = source.readframes(maximum)
        if not raw:
            return
        count = len(raw) // 2
        cut = count
        if count == maximum and source.tell() < source.getnframes():
            samples = array("h", raw[28 * rate * 2:])
            if sys.byteorder != "little":
                samples.byteswap()
            energies = [
                sum(value * value for value in samples[start:start + window])
                for start in range(0, len(samples) - window + 1, window)
            ]
            # Prefer a genuine quiet interval; steady sound keeps the 30s cut.
            quiet = min(energies)
            if quiet * len(energies) < sum(energies) / 4:
                index = max(i for i, energy in enumerate(energies) if energy == quiet)
                cut = 28 * rate + index * window + window // 2
                source.setpos(offset + cut)
        yield raw[:cut * 2], offset / rate


def original_word_texts(items, source):
    # Match the aligner's lexical tokens back to the ASR text. Keep original
    # punctuation/spacing; refuse a mismatch rather than dropping speech.
    def lexical(char):
        return char == "'" or unicodedata.category(char)[0] in "LN"
    positions = [index for index, char in enumerate(source) if lexical(char)]
    original = "".join(source[index] for index in positions)
    tokens = ["".join(char for char in item.text if lexical(char)) for item in items]
    if any(not token for token in tokens) or "".join(tokens) != original:
        raise ValueError("Qwen alignment text mismatch")
    words, cursor, previous = [], 0, 0
    for token in tokens:
        cursor += len(token)
        boundary = positions[cursor] if cursor < len(positions) else len(source)
        words.append(source[previous:boundary])
        previous = boundary
    return words


def aligned_parts(items, duration, source):
    if not items:
        if source.strip():
            raise ValueError("missing Qwen alignment")
        return []
    words = original_word_texts(items, source)
    parts = []
    start, end, text = 0.0, 0.0, ""
    previous = -1.0
    for item, word in zip(items, words):
        a, b = item.start_time, item.end_time
        if (not math.isfinite(a) or not math.isfinite(b) or a < 0
                or a < previous or b < a or b > duration + .05):
            raise ValueError("invalid Qwen alignment")
        previous = a
        if not word.strip():
            continue
        if text and end > start and (a - end >= .8 or b - start > 8
                                    or len(text + word) > 48):
            parts.append((start, end, text))
            text = ""
        if not text:
            start = a
        text += word
        end = min(b, duration)
        if end > start and text.rstrip().endswith(("。", "！", "？", ".", "!", "?")):
            parts.append((start, end, text))
            text = ""
    if text:
        if end <= start:
            raise ValueError("empty Qwen alignment interval")
        parts.append((start, end, text))
    return parts


def main():
    import numpy as np
    import torch
    from qwen_asr import Qwen3ASRModel

    model_path, audio, language, output, aligner_path = sys.argv[1:]
    if not Path(model_path).is_dir() or not Path(aligner_path).is_dir():
        raise ValueError("local Qwen model directories required")
    torch.set_num_threads(4)
    # Conservative CPU runtime: no unverified GPU/MPS fallback or downloads.
    options = dict(dtype=torch.float32, device_map="cpu", local_files_only=True)
    model = Qwen3ASRModel.from_pretrained(
        model_path, forced_aligner=aligner_path, forced_aligner_kwargs=options,
        max_inference_batch_size=1, max_new_tokens=1024, **options)
    languages = {"zh": "Chinese", "en": "English", "ja": "Japanese", "ko": "Korean"}
    cues, detected = [], language
    with wave.open(audio, "rb") as source:
        if (source.getnchannels() != 1 or source.getsampwidth() != 2
                or source.getframerate() != 16000 or source.getnframes() > 6 * 3600 * 16000):
            raise ValueError("invalid normalized audio")
        for raw, offset in audio_blocks(source):
            samples = np.frombuffer(raw, dtype="<i2").astype(np.float32) / 32768.0
            duration = len(samples) / 16000
            if float(np.max(np.abs(samples))) > 10 / 32768:
                result = model.transcribe(audio=(samples, 16000),
                                         language=languages.get(language), return_time_stamps=True)[0]
                if language == "auto":
                    detected = next((code for code, name in languages.items() if name == result.language), "auto")
                if result.text.strip() and result.time_stamps is None:
                    raise ValueError("missing Qwen alignment")
                for start, end, text in aligned_parts(result.time_stamps.items if result.time_stamps else [], duration, result.text):
                    a, b = round((offset + start) * 1000), round((offset + end) * 1000)
                    if b > a:
                        cues.append({"offsets": {"from": a, "to": b}, "text": text})
                    if len(cues) > 10000:
                        raise ValueError("too many subtitles")
    Path(output).write_text(json.dumps({"result": {"language": detected}, "transcription": cues}), encoding="utf-8")


if __name__ == "__main__":
    main()
