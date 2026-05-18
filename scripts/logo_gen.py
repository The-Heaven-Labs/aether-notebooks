from PIL import Image, ImageDraw

IMG_SIZE = 1200
SIZE = 32

img = Image.new("RGBA", (IMG_SIZE, IMG_SIZE), (11, 15, 26, 255))
draw = ImageDraw.Draw(img)

scale = IMG_SIZE // SIZE


def draw_icon(x, y, w, h, color):
    rx = max(2, w * scale // 16)
    x1, y1 = x * scale, y * scale
    draw.rounded_rectangle(
        [x1, y1, x1 + w * scale, y1 + h * scale], radius=rx, fill=color
    )


ACTIVE = (99, 102, 241, 255)
WHITE = (248, 250, 252, 255)

draw_icon(3, 3, 12, 12, ACTIVE)
draw_icon(17, 3, 12, 12, ACTIVE)
draw_icon(3, 17, 5, 5, (99, 102, 241, 102))
draw_icon(10, 17, 5, 5, ACTIVE)
draw_icon(17, 17, 5, 5, (99, 102, 241, 102))
draw_icon(24, 17, 5, 5, (99, 102, 241, 102))
draw_icon(3, 24, 5, 5, (99, 102, 241, 102))
draw_icon(10, 24, 5, 5, (99, 102, 241, 102))
draw_icon(17, 24, 5, 5, (99, 102, 241, 102))
draw_icon(24, 24, 5, 5, (99, 102, 241, 102))
draw.ellipse(
    [10.5 * scale - 18, 10.5 * scale - 18, 10.5 * scale + 18, 10.5 * scale + 18],
    fill=WHITE,
)

img.save("logo.png")
print("Saved logo.png")
