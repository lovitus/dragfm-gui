"""Clock-based pacing for a disposable test relay; never changes app behavior."""
from __future__ import annotations
import math
import time
from collections.abc import Callable


class ByteRatePacer:
    def __init__(self, rate: float, *, burst: int = 256 * 1024,
                 clock: Callable[[], float] = time.monotonic) -> None:
        if not math.isfinite(rate) or rate <= 0 or burst < 0:
            raise ValueError('A finite positive byte rate and nonnegative burst are required')
        self.rate = rate
        self.burst_seconds = burst / rate
        self.clock = clock
        self.deadline = clock()

    def delay(self, count: int) -> float:
        if count < 0:
            raise ValueError('Byte count cannot be negative')
        now = self.clock()
        # Charge bytes to an absolute budget. Oversleeping one short wait must
        # reduce subsequent waits, not add scheduler latency to every packet.
        # Bound accumulated idle credit so a long pause cannot allow a whole
        # transfer to bypass the intended rate limit.
        self.deadline = max(self.deadline, now - self.burst_seconds) + count / self.rate
        return max(0.0, self.deadline - now)
