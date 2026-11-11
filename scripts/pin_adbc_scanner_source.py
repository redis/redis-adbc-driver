"""Pin the scanner's Arrow ADBC 24 overlay to the official release commit."""
from pathlib import Path
import sys


OLD_REF = 'REF apache-arrow-adbc-${VERSION}'
OLD_HASH = ('2c325413c4af45642d956263f6a3e56012d7468cc3edf7a4dad325d85aab9469d'
            '066af42036983d770f2d8fc366841652455c63cb242ecc4b57b35481795c1cf')
# apache/arrow-adbc tag apache-arrow-adbc-24 resolves to this commit.
COMMIT = 'a3f81c6feb1133ba80600ac32e6d7c0b0b407e18'
SHA512 = ('73bae8559e3bd1fa6826d0d384df6556eb1a77fbef05f85d33c8cfd77d81adaaa'
          '0a80ef6bb743bdbf4cc3be8f5f6e0f1408bd0f00d55cf3a5f8f28c13cb7cac6')


def pin(path):
    path = Path(path)
    source = path.read_text()
    if source.count(OLD_REF) != 1 or source.count('SHA512 ' + OLD_HASH) != 1:
        raise ValueError('Unexpected scanner Arrow ADBC overlay; review its source pin')
    source = source.replace(OLD_REF, 'REF ' + COMMIT)
    source = source.replace('SHA512 ' + OLD_HASH, 'SHA512 ' + SHA512)
    path.write_text(source)


if __name__ == '__main__':
    pin(sys.argv[1])
