"""Offline checks: uv run --locked --project python python -m unittest discover -s python."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import numpy as np

import runtime


class RuntimeTests(unittest.TestCase):
    def test_long_digital_silence_is_removed_before_features(self):
        # Enrollment can include long pauses that sentence-based matching omits.
        speech = np.random.default_rng(9).normal(0, 0.03, 64000).astype(np.float32)
        wave = np.concatenate((np.zeros(16000), speech, np.zeros(32000), speech, np.zeros(16000)))
        actual = runtime.remove_long_silence(wave)
        expected = np.concatenate((np.zeros(960), speech, np.zeros(1920), speech, np.zeros(960)))
        np.testing.assert_array_equal(actual, expected)

    def test_silence_filter_preserves_short_pauses_and_quiet_audio(self):
        quiet = np.full(64000, 0.0001, dtype=np.float32)
        wave = np.concatenate((quiet, np.zeros(3200, dtype=np.float32), quiet))
        np.testing.assert_array_equal(runtime.remove_long_silence(wave), wave)

    def test_silence_does_not_count_toward_minimum_speech_duration(self):
        with tempfile.NamedTemporaryFile() as audio:
            wave = np.concatenate((np.ones(32000, dtype=np.float32), np.zeros(48000, dtype=np.float32)))
            with patch.object(runtime, 'decode', return_value=wave), patch.object(runtime, 'load_model') as load:
                with self.assertRaises(runtime.VoxError) as raised:
                    runtime.run({'audio': audio.name, 'segments': [{'id': 'test', 'start': 0, 'end': 5}]})
                self.assertEqual(raised.exception.code, 'AUDIO_TOO_SHORT')
                load.assert_not_called()

    def test_kaldi_fbank_matches_independent_numpy(self):
        audio = np.random.default_rng(3).normal(0, 0.03, 48000).astype(np.float32)
        frames = np.lib.stride_tricks.sliding_window_view(audio * 32768, 400)[::160].copy()
        frames -= frames.mean(axis=1, keepdims=True)
        frames = np.concatenate((0.03 * frames[:, :1], frames[:, 1:] - 0.97 * frames[:, :-1]), axis=1)
        power = np.abs(np.fft.rfft(frames * np.hamming(400), n=512)) ** 2
        mel = lambda x: 1127 * np.log1p(x / 700)
        edges = np.linspace(mel(20), mel(8000), 82)
        freq = mel(np.arange(257) * 31.25)
        bins = np.maximum(0, np.minimum((freq - edges[:-2, None]) / np.diff(edges)[:-1, None], (edges[2:, None] - freq) / np.diff(edges)[1:, None]))
        bins[:, -1] = 0
        reference = np.log(np.maximum(power @ bins.T, np.finfo(np.float32).eps))
        reference -= reference.mean(axis=0, keepdims=True)
        actual = np.array(runtime.fbank(audio))
        self.assertEqual(actual.shape, (298, 80))
        np.testing.assert_allclose(actual, reference, atol=7e-4)

    def test_silent_features_are_finite(self):
        features = np.array(runtime.fbank(np.zeros(48000, dtype=np.float32)))
        self.assertTrue(np.isfinite(features).all())
        np.testing.assert_allclose(features, 0, atol=1e-5)

    def test_strict_load_rejects_missing_weights(self):
        with self.assertRaises(ValueError):
            runtime.SpeakerModel().load_weights([], strict=True)

    def test_silence_rejected_before_model_download(self):
        with tempfile.TemporaryDirectory() as temporary:
            audio = Path(temporary) / 'empty.wav'
            audio.touch()
            with patch.object(runtime, 'decode', return_value=np.zeros(48000, dtype=np.float32)), patch.object(runtime, 'load_model') as load:
                with self.assertRaises(runtime.VoxError) as raised:
                    runtime.run({'audio': str(audio)})
                self.assertEqual(raised.exception.code, 'AUDIO_SILENT')
                load.assert_not_called()

    def test_invalid_segments_rejected_before_decode(self):
        with tempfile.NamedTemporaryFile() as audio:
            for start, end in [(-1, 5), (0, 31), (0, 2), (float('nan'), 4)]:
                with self.subTest(start=start, end=end), self.assertRaises(runtime.VoxError):
                    runtime.run({'audio': audio.name, 'segments': [{'id': 'test', 'start': start, 'end': end}]})

    def test_short_audio_is_clamped_and_rejected(self):
        with tempfile.NamedTemporaryFile() as audio:
            with patch.object(runtime, 'decode', return_value=np.ones(16000, dtype=np.float32)), self.assertRaises(runtime.VoxError) as raised:
                runtime.run({'audio': audio.name})
            self.assertEqual(raised.exception.code, 'AUDIO_TOO_SHORT')


if __name__ == '__main__':
    unittest.main()
