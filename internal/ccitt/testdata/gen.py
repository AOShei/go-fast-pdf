"""Makes the test data of package ccitt: one bilevel image, coded by
libtiff's fax coders (through tiffcp) in each way a PDF can give it.

    python3 gen.py   (needs Pillow and tiffcp)

image.pbm is the image; NAME.bin is its coded data and NAME.json the
parameters of the CCITTFaxDecode filter that read it. The image's bits
are 1 for white, so the coded data is read with BlackIs1 (libtiff codes
the runs of 0 bits as white).
"""
import json, os, random, subprocess, tempfile
from PIL import Image, ImageDraw

W, H = 203, 61  # a width that is not a whole number of bytes
img = Image.new("1", (W, H), 1)
d = ImageDraw.Draw(img)
d.text((4, 2), "Fax 1234 ABCDEFG abcdefg", fill=0)
d.rectangle((150, 5, 198, 40), outline=0)
d.line((0, 60, 202, 30), fill=0)
d.ellipse((60, 25, 120, 58), outline=0, width=3)
rnd = random.Random(7)
for _ in range(120):  # specks, which make short runs
    img.putpixel((rnd.randrange(W), rnd.randrange(H)), 0)
for x in range(W):  # a row of alternating pixels, the shortest runs
    img.putpixel((x, 20), x % 2)
img.save("image.pbm")

cases = {
    "g4": (["-c", "g4"], {"K": -1}),
    "g3-1d": (["-c", "g3:1d"], {"K": 0}),
    "g3-1d-fill": (["-c", "g3:1d:fill"], {"K": 0}),
    "g3-2d": (["-c", "g3:2d"], {"K": 4}),
    "g3-2d-fill": (["-c", "g3:2d:fill"], {"K": 4}),
}
with tempfile.TemporaryDirectory() as tmp:
    src = os.path.join(tmp, "src.tif")
    img.save(src, compression="raw")
    for name, (args, parms) in cases.items():
        out = os.path.join(tmp, name + ".tif")
        subprocess.run(["tiffcp", *args, "-r", str(H), src, out], check=True)
        t = Image.open(out)
        off, n = t.tag_v2[273][0], t.tag_v2[279][0]
        with open(out, "rb") as f:
            f.seek(off)
            data = f.read(n)
        with open(name + ".bin", "wb") as f:
            f.write(data)
        parms.update({"Columns": W, "Rows": H, "BlackIs1": True})
        with open(name + ".json", "w") as f:
            json.dump(parms, f)
    # Modified Huffman without ends of line, each row on a byte: TIFF's
    # compression 2.
    out = os.path.join(tmp, "mh.tif")
    img.save(out, compression="tiff_ccitt")
    t = Image.open(out)
    with open(out, "rb") as f:
        data = b""
        for off, n in zip(t.tag_v2[273], t.tag_v2[279]):
            f.seek(off)
            data += f.read(n)
    with open("mh.bin", "wb") as f:
        f.write(data)
    with open("mh.json", "w") as f:
        json.dump({"K": 0, "Columns": W, "Rows": H, "BlackIs1": True, "EncodedByteAlign": True}, f)
