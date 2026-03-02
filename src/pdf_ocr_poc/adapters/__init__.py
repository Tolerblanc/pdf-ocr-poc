from .apple_vision import AppleVisionOCRAdapter
from .base import OCRAdapter, OCRRunOutput
from .paddle import PaddleOCRAdapter
from .tesseract import TesseractOCRAdapter

__all__ = [
    "OCRAdapter",
    "OCRRunOutput",
    "AppleVisionOCRAdapter",
    "PaddleOCRAdapter",
    "TesseractOCRAdapter",
]
