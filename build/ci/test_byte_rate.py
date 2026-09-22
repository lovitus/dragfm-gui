"""Fixture-only clock tests; native application acceptance remains unmocked."""
import unittest
from byte_rate import ByteRatePacer


class PacingTests(unittest.TestCase):
    def test_scheduler_oversleep_is_not_repeated_for_every_chunk(self):
        now = [0.0]
        pacer = ByteRatePacer(102400, burst=16384, clock=lambda: now[0])
        for _ in range(100):
            delay = pacer.delay(1024)
            if delay:
                # Model a scheduler that wakes 40ms late on short waits.
                now[0] += delay + .04
        self.assertGreaterEqual(now[0], .99)
        self.assertLessEqual(now[0], 1.06)
        # Repeated relative sleeps would take five seconds for the same bytes.

    def test_normal_rate_accounts_for_actual_elapsed_io_time(self):
        now = [0.0]
        pacer = ByteRatePacer(102400, burst=16384, clock=lambda: now[0])
        for _ in range(100):
            now[0] += .003
            now[0] += pacer.delay(1024)
        self.assertAlmostEqual(now[0], 1.0, places=6)

    def test_idle_credit_is_bounded(self):
        now = [0.0]
        pacer = ByteRatePacer(1024, burst=128, clock=lambda: now[0])
        now[0] = 1000.0
        self.assertAlmostEqual(pacer.delay(1024), .875)

    def test_invalid_inputs_fail(self):
        for rate in [0, -1, float('inf'), float('nan')]:
            with self.assertRaises(ValueError):
                ByteRatePacer(rate)
        with self.assertRaises(ValueError):
            ByteRatePacer(1).delay(-1)


if __name__ == '__main__':
    unittest.main()
