"""Download only after an explicit Tangentry model download request."""
import json
import sys
from pathlib import Path
from huggingface_hub import HfApi, snapshot_download

repository, destination, backend = sys.argv[1:]
revision = HfApi(token=False).model_info(repository).sha
patterns = (["*.json", "*.safetensors", "*.txt", "*.model", "*.tiktoken"]
            if backend == "qwen3-asr" else
            ["config.json", "model.bin", "tokenizer.json", "preprocessor_config.json", "vocabulary.*"])
snapshot_download(repository, local_dir=destination, revision=revision, token=False,
                  allow_patterns=patterns, max_workers=2)
Path(destination, "tangentry-source.json").write_text(json.dumps({"repository": repository, "revision": revision}), encoding="utf-8")
