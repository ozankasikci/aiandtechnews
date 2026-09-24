// Usage: cutout <input> <output.png>
// Cuts out the foreground subject (person) with Vision and writes a PNG with alpha.
// The featured-image pipeline runs it (CUTOUT_BIN) for the public-figure collage.
//
// Build (macOS 14+, Xcode command line tools):
//   swiftc -O -o ~/aiandtechnews/bin/cutout tools/cutout/cutout.swift
//
// Exit codes: 0 ok (prints "ok instances=N size=WxH"), 2 usage or unreadable
// input, 3 no subject found, 4 could not render, 1 any other error.
import Foundation
import Vision
import CoreImage
import CoreImage.CIFilterBuiltins

let args = CommandLine.arguments
guard args.count == 3, let input = CIImage(contentsOf: URL(fileURLWithPath: args[1])) else {
    FileHandle.standardError.write("usage: cutout <input> <output.png>\n".data(using: .utf8)!); exit(2)
}
let request = VNGenerateForegroundInstanceMaskRequest()
let handler = VNImageRequestHandler(ciImage: input)
do {
    try handler.perform([request])
    guard let result = request.results?.first else { print("no subject found"); exit(3) }
    let mask = try result.generateScaledMaskForImage(forInstances: result.allInstances, from: handler)
    var maskImage = CIImage(cvPixelBuffer: mask)
    // Remove watermarks and captions: blank out any detected text (plus a
    // margin that also covers a logo mark beside it) from the subject mask.
    let recognize = VNRecognizeTextRequest()
    recognize.recognitionLevel = .accurate
    recognize.usesLanguageCorrection = false
    recognize.minimumTextHeight = 0.01
    let rectangles = VNDetectTextRectanglesRequest()
    try handler.perform([recognize, rectangles])
    let extent = input.extent
    let boxes = (recognize.results ?? []).map(\.boundingBox) + (rectangles.results ?? []).map(\.boundingBox)
    // Watermarks and captions are small; a large "text" box is a false positive on the subject.
    for b in boxes where b.height < 0.08 && b.width < 0.35 {
        let box = CGRect(x: b.minX * extent.width, y: b.minY * extent.height,
                         width: b.width * extent.width, height: b.height * extent.height)
        let grown = box.insetBy(dx: -box.height * 1.6, dy: -box.height * 0.6)
        let black = CIImage(color: .black).cropped(to: grown)
        maskImage = black.composited(over: maskImage)
        print("removed text region at \(Int(box.minX)),\(Int(box.minY)) \(Int(box.width))x\(Int(box.height))")
    }
    // Publishers stamp their watermark in a bottom corner: drop both bottom corners.
    let cornerW = extent.width * 0.22, cornerH = extent.height * 0.11
    for corner in [CGRect(x: 0, y: 0, width: cornerW, height: cornerH),
                   CGRect(x: extent.width - cornerW, y: 0, width: cornerW, height: cornerH)] {
        maskImage = CIImage(color: .black).cropped(to: corner).composited(over: maskImage)
    }
    maskImage = maskImage.cropped(to: extent)
    let blend = CIFilter.blendWithMask()
    blend.inputImage = input
    blend.backgroundImage = CIImage.empty()
    blend.maskImage = maskImage
    let context = CIContext()
    guard let output = blend.outputImage,
          let cg = context.createCGImage(output, from: input.extent) else { exit(4) }
    let url = URL(fileURLWithPath: args[2]) as CFURL
    let dest = CGImageDestinationCreateWithURL(url, "public.png" as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, cg, nil)
    CGImageDestinationFinalize(dest)
    print("ok instances=\(result.allInstances.count) size=\(cg.width)x\(cg.height)")
} catch {
    print("error: \(error)"); exit(1)
}
