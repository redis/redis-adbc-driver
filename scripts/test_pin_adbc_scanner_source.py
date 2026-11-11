from pathlib import Path
import tempfile
import unittest

import pin_adbc_scanner_source as source_pin


class ScannerSourcePinTests(unittest.TestCase):
    def test_pin_retains_rest_of_overlay(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'portfile.cmake'
            path.write_text(f'{source_pin.OLD_REF}\nSHA512 {source_pin.OLD_HASH}\nPATCHES toml.patch\n')
            source_pin.pin(path)
            self.assertEqual(path.read_text(),
                             f'REF {source_pin.COMMIT}\nSHA512 {source_pin.SHA512}\nPATCHES toml.patch\n')

    def test_unexpected_overlay_is_not_modified(self):
        for source in ('REF newer\nSHA512 unknown',
                       f'{source_pin.OLD_REF}\nSHA512 {source_pin.OLD_HASH}\n{source_pin.OLD_REF}'):
            with self.subTest(source=source), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'portfile.cmake'
                path.write_text(source)
                with self.assertRaises(ValueError):
                    source_pin.pin(path)
                self.assertEqual(path.read_text(), source)


if __name__ == '__main__':
    unittest.main()
