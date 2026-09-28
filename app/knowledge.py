"""Parsing of uploaded material into knowledge-base text."""


class ParseError(ValueError):
    """Raised when an uploaded file cannot be parsed into usable text."""


def parse_text(raw: bytes) -> str:
    """Decode ``raw`` bytes as UTF-8 plain text.

    Raises :class:`ParseError` for non-UTF-8 or effectively empty content so
    callers can reject the upload before writing anything.
    """
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ParseError("file is not valid UTF-8 text") from exc
    if not text.strip():
        raise ParseError("file is empty")
    return text
