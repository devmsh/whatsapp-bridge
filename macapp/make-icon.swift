#!/usr/bin/env swift
import AppKit

// Generates AppIcon.iconset/*.png. build.sh turns it into AppIcon.icns via
// iconutil. Keeps the repo free of committed binary art.

let outDir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "./AppIcon.iconset"
try? FileManager.default.createDirectory(
    atPath: outDir, withIntermediateDirectories: true)

/// Draws the icon at `size` points into a bitmap.
func render(size: Int) -> Data? {
    guard
        let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)
    else { return nil }

    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)

    let s = CGFloat(size)
    // macOS app icons sit in a squircle inset from the full canvas.
    let inset = s * 0.09
    let rect = NSRect(x: inset, y: inset, width: s - inset * 2, height: s - inset * 2)
    let radius = rect.width * 0.2237  // Apple's continuous-corner ratio

    let squircle = NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius)

    let gradient = NSGradient(
        colors: [
            NSColor(srgbRed: 0.13, green: 0.83, blue: 0.44, alpha: 1),  // WhatsApp green
            NSColor(srgbRed: 0.02, green: 0.55, blue: 0.45, alpha: 1),  // deep teal
        ])!
    gradient.draw(in: squircle, angle: -90)

    // Chat bubble glyph, centred.
    let config = NSImage.SymbolConfiguration(
        pointSize: rect.width * 0.46, weight: .semibold)
    if let symbol = NSImage(
        systemSymbolName: "bubble.left.and.bubble.right.fill",
        accessibilityDescription: nil)?
        .withSymbolConfiguration(config)
    {
        let tinted = NSImage(size: symbol.size)
        tinted.lockFocus()
        NSColor.white.set()
        NSRect(origin: .zero, size: symbol.size).fill(using: .sourceOver)
        symbol.draw(
            at: .zero, from: NSRect(origin: .zero, size: symbol.size),
            operation: .destinationIn, fraction: 1)
        tinted.unlockFocus()

        let glyphRect = NSRect(
            x: rect.midX - tinted.size.width / 2,
            y: rect.midY - tinted.size.height / 2,
            width: tinted.size.width,
            height: tinted.size.height)
        tinted.draw(in: glyphRect)
    }

    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])
}

// The sizes iconutil expects.
let variants: [(name: String, px: Int)] = [
    ("icon_16x16", 16), ("icon_16x16@2x", 32),
    ("icon_32x32", 32), ("icon_32x32@2x", 64),
    ("icon_128x128", 128), ("icon_128x128@2x", 256),
    ("icon_256x256", 256), ("icon_256x256@2x", 512),
    ("icon_512x512", 512), ("icon_512x512@2x", 1024),
]

for v in variants {
    guard let data = render(size: v.px) else {
        FileHandle.standardError.write("failed to render \(v.name)\n".data(using: .utf8)!)
        exit(1)
    }
    try data.write(to: URL(fileURLWithPath: "\(outDir)/\(v.name).png"))
}
print("wrote \(variants.count) icon variants to \(outDir)")
