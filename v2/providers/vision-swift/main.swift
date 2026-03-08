import Foundation
import PDFKit
import Vision
import CoreGraphics
import CoreText

struct Request: Decodable {
    let input_pdf: String
    let output_dir: String
    let profile: String
    let local_only: Bool
    let max_workers: Int
    let workers_mode: String
    let request_source: String?
}

struct Result: Encodable {
    let searchable_pdf: String
    let pages_json: String
    let text_path: String
    let markdown_path: String
    let stage_timings: [String: Double]
    let warnings: [String]
}

struct BBox: Encodable {
    let x0: Double
    let y0: Double
    let x1: Double
    let y1: Double
}

struct OCRBlock: Encodable {
    let text: String
    let bbox: BBox
    let blockType: String
    let confidence: Double
    let readingOrder: Int

    enum CodingKeys: String, CodingKey {
        case text
        case bbox
        case blockType = "block_type"
        case confidence
        case readingOrder = "reading_order"
    }
}

struct OCRPage: Encodable {
    let page: Int
    let width: Int
    let height: Int
    let isBlank: Bool
    let text: String
    let blocks: [OCRBlock]

    enum CodingKeys: String, CodingKey {
        case page
        case width
        case height
        case isBlank = "is_blank"
        case text
        case blocks
    }
}

struct OCRDocument: Encodable {
    let engine: String
    let sourcePDF: String
    let pages: [OCRPage]

    enum CodingKeys: String, CodingKey {
        case engine
        case sourcePDF = "source_pdf"
        case pages
    }
}

struct RecognizedLine {
    let text: String
    let confidence: Double
    let bbox: BBox
}

struct PageArtifact {
    let index: Int
    let pageBounds: CGRect
    let imageWidth: Int
    let imageHeight: Int
    let ocrPage: OCRPage
}

enum ProviderError: Error {
    case invalidRequest
    case invalidInputPDF(String)
    case failedToLoadPDF(String)
    case failedToRenderPage(Int)
    case failedToRecognizePage(Int)
}

let chapterSuffixRegex = try! NSRegularExpression(pattern: "\\b(\\d{1,2})\\s*[\\uAC15\\uC794\\uC815]\\b")
let restPathRegex = try! NSRegularExpression(pattern: "\\b(GET|POST|PUT|DELETE)\\s+users/(\\d+)", options: [.caseInsensitive])
let headingRegex = try! NSRegularExpression(pattern: "^(\\d+\\s*\\uC7A5|chapter\\s+\\d+|part\\s+\\d+)", options: [.caseInsensitive])
let captionRegex = try! NSRegularExpression(pattern: "^(\\uADF8\\uB9BC|\\uD45C|fig\\.?|figure|table)\\s*", options: [.caseInsensitive])
let strongCodeRegex = try! NSRegularExpression(pattern: "(\\bdef\\b|\\bclass\\b|\\breturn\\b|\\bimport\\b|\\bSELECT\\b|\\bFROM\\b|\\bWHERE\\b|\\bif\\b|\\bfor\\b|\\bwhile\\b|[{}\\[\\];]|=>|==|!=)", options: [.caseInsensitive])
let jsonKeyRegex = try! NSRegularExpression(pattern: "^\\s*\"[A-Za-z0-9_]+\"\\s*:")

func writeFile(path: String, content: Data) throws {
    let url = URL(fileURLWithPath: path)
    try FileManager.default.createDirectory(
        at: url.deletingLastPathComponent(),
        withIntermediateDirectories: true
    )
    try content.write(to: url)
}

func normalizeVisionText(_ raw: String) -> String {
    var value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    let fullRange = NSRange(value.startIndex..<value.endIndex, in: value)
    value = chapterSuffixRegex.stringByReplacingMatches(in: value, range: fullRange, withTemplate: "$1\\uC7A5")
    let nextRange = NSRange(value.startIndex..<value.endIndex, in: value)
    value = restPathRegex.stringByReplacingMatches(in: value, range: nextRange, withTemplate: "$1 /users/$2")
    return value
}

func symbolRatio(_ text: String) -> Double {
    let symbols = Set("{}[]();=<>:+-*/_.`\"'\\")
    let chars = text.trimmingCharacters(in: .whitespacesAndNewlines)
    if chars.isEmpty {
        return 0
    }
    let count = chars.filter { symbols.contains($0) }.count
    return Double(count) / Double(chars.count)
}

func classifyBlock(_ text: String) -> String {
    let stripped = text.trimmingCharacters(in: .whitespacesAndNewlines)
    if stripped.isEmpty {
        return "paragraph"
    }

    let nsRange = NSRange(stripped.startIndex..<stripped.endIndex, in: stripped)
    if headingRegex.firstMatch(in: stripped, range: nsRange) != nil {
        return "heading"
    }
    if captionRegex.firstMatch(in: stripped, range: nsRange) != nil {
        return "caption"
    }

    let strongCode = strongCodeRegex.firstMatch(in: stripped, range: nsRange) != nil
        || jsonKeyRegex.firstMatch(in: stripped, range: nsRange) != nil
        || stripped.uppercased().hasPrefix("GET /")
        || stripped.uppercased().hasPrefix("POST /")
        || stripped.uppercased().hasPrefix("PUT /")
        || stripped.uppercased().hasPrefix("DELETE /")

    if strongCode || symbolRatio(stripped) >= 0.12 {
        return "code"
    }

    return "paragraph"
}

func renderScale(for profile: String) -> CGFloat {
    switch profile.lowercased() {
    case "quality":
        return 3.0
    case "fast":
        return 2.0
    default:
        return 2.4
    }
}

func renderPageImage(page: PDFPage, scale: CGFloat, pageNumber: Int) throws -> CGImage {
    guard let pageRef = page.pageRef else {
        throw ProviderError.failedToRenderPage(pageNumber)
    }

    let mediaBox = pageRef.getBoxRect(.mediaBox)
    let pixelWidth = max(1, Int(mediaBox.width * scale))
    let pixelHeight = max(1, Int(mediaBox.height * scale))

    let colorSpace = CGColorSpaceCreateDeviceRGB()
    guard let context = CGContext(
        data: nil,
        width: pixelWidth,
        height: pixelHeight,
        bitsPerComponent: 8,
        bytesPerRow: 0,
        space: colorSpace,
        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
    ) else {
        throw ProviderError.failedToRenderPage(pageNumber)
    }

    context.setFillColor(CGColor(gray: 1, alpha: 1))
    context.fill(CGRect(x: 0, y: 0, width: CGFloat(pixelWidth), height: CGFloat(pixelHeight)))

    context.saveGState()
    let drawRect = CGRect(x: 0, y: 0, width: CGFloat(pixelWidth), height: CGFloat(pixelHeight))
    let transform = pageRef.getDrawingTransform(.mediaBox, rect: drawRect, rotate: 0, preserveAspectRatio: true)
    context.concatenate(transform)
    context.drawPDFPage(pageRef)
    context.restoreGState()

    guard let image = context.makeImage() else {
        throw ProviderError.failedToRenderPage(pageNumber)
    }
    return image
}

func recognizeLines(cgImage: CGImage, pageNumber: Int) throws -> [RecognizedLine] {
    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = false

    let preferredLanguages = ["ko-KR", "ko", "en-US", "en-GB", "en"]
    if let supported = try? request.supportedRecognitionLanguages() {
        let selected = preferredLanguages.filter { supported.contains($0) }
        if !selected.isEmpty {
            request.recognitionLanguages = selected
        }
    }

    let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
    do {
        try handler.perform([request])
    } catch {
        throw ProviderError.failedToRecognizePage(pageNumber)
    }

    guard let observations = request.results else {
        return []
    }

    let width = Double(cgImage.width)
    let height = Double(cgImage.height)

    var lines: [RecognizedLine] = []
    for observation in observations {
        guard let candidate = observation.topCandidates(1).first else {
            continue
        }
        let text = candidate.string.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty {
            continue
        }

        let box = observation.boundingBox
        let x0 = Double(box.origin.x) * width
        let y0 = (1 - (Double(box.origin.y) + Double(box.height))) * height
        let x1 = (Double(box.origin.x) + Double(box.width)) * width
        let y1 = (1 - Double(box.origin.y)) * height

        lines.append(
            RecognizedLine(
                text: text,
                confidence: Double(candidate.confidence),
                bbox: BBox(x0: x0, y0: y0, x1: x1, y1: y1)
            )
        )
    }

    return lines.sorted {
        if $0.bbox.y0 == $1.bbox.y0 {
            return $0.bbox.x0 < $1.bbox.x0
        }
        return $0.bbox.y0 < $1.bbox.y0
    }
}

func processPage(page: PDFPage, pageNumber: Int, scale: CGFloat) throws -> PageArtifact {
    let cgImage = try renderPageImage(page: page, scale: scale, pageNumber: pageNumber)
    let lines = try recognizeLines(cgImage: cgImage, pageNumber: pageNumber)

    var blocks: [OCRBlock] = []
    for (index, line) in lines.enumerated() {
        let normalizedText = normalizeVisionText(line.text)
        blocks.append(
            OCRBlock(
                text: normalizedText,
                bbox: line.bbox,
                blockType: classifyBlock(normalizedText),
                confidence: line.confidence,
                readingOrder: index + 1
            )
        )
    }

    let pageText = blocks.map { $0.text }.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    let bounds = page.bounds(for: .mediaBox)

    let ocrPage = OCRPage(
        page: pageNumber,
        width: cgImage.width,
        height: cgImage.height,
        isBlank: pageText.isEmpty,
        text: pageText,
        blocks: blocks
    )

    return PageArtifact(
        index: pageNumber - 1,
        pageBounds: bounds,
        imageWidth: cgImage.width,
        imageHeight: cgImage.height,
        ocrPage: ocrPage
    )
}

func pdfRect(for block: OCRBlock, artifact: PageArtifact) -> CGRect {
    let imageWidth = Double(max(1, artifact.imageWidth))
    let imageHeight = Double(max(1, artifact.imageHeight))
    let pageWidth = Double(artifact.pageBounds.width)
    let pageHeight = Double(artifact.pageBounds.height)

    let left = block.bbox.x0 / imageWidth * pageWidth
    let right = block.bbox.x1 / imageWidth * pageWidth
    let top = block.bbox.y0 / imageHeight * pageHeight
    let bottom = block.bbox.y1 / imageHeight * pageHeight

    let pdfX = left
    let pdfY = max(0, pageHeight - bottom)
    let pdfW = max(1, right - left)
    let pdfH = max(1, bottom - top)
    return CGRect(x: pdfX, y: pdfY, width: pdfW, height: pdfH)
}

func drawInvisibleText(_ text: String, in rect: CGRect, context: CGContext) {
    let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
    if trimmed.isEmpty || rect.width < 1 || rect.height < 1 {
        return
    }

    let fontSize = max(6, min(24, rect.height * 0.9))
    let font = CTFontCreateWithName("AppleSDGothicNeo-Regular" as CFString, fontSize, nil)
    let attributes: [NSAttributedString.Key: Any] = [
        NSAttributedString.Key(kCTFontAttributeName as String): font,
    ]
    let attributed = NSAttributedString(string: trimmed, attributes: attributes)
    let line = CTLineCreateWithAttributedString(attributed)

    context.saveGState()
    context.textMatrix = .identity
    context.setTextDrawingMode(.fill)
    context.setFillColor(CGColor(red: 0, green: 0, blue: 0, alpha: 0.002))
    let baselineY = rect.minY + max(0, (rect.height - fontSize) * 0.5)
    context.textPosition = CGPoint(x: rect.minX, y: baselineY)
    CTLineDraw(line, context)
    context.restoreGState()
}

func copySearchablePDFFallback(inputPDF: String, outputPDF: String, warnings: [String]) -> (String, [String]) {
    var nextWarnings = warnings
    do {
        if FileManager.default.fileExists(atPath: outputPDF) {
            try FileManager.default.removeItem(atPath: outputPDF)
        }
        try FileManager.default.copyItem(atPath: inputPDF, toPath: outputPDF)
        nextWarnings.append("searchable_pdf_method=copy-fallback")
        return ("copy-fallback", nextWarnings)
    } catch {
        nextWarnings.append("searchable_pdf_write_failed")
        return ("none", nextWarnings)
    }
}

func buildSearchablePDF(inputPDF: String, outputPDF: String, artifacts: [PageArtifact]) -> (String, [String]) {
    guard let sourceURL = CFURLCreateWithFileSystemPath(nil, inputPDF as CFString, .cfurlposixPathStyle, false),
          let source = CGPDFDocument(sourceURL)
    else {
        return copySearchablePDFFallback(
            inputPDF: inputPDF,
            outputPDF: outputPDF,
            warnings: ["failed_to_open_input_pdf_for_text_layer"]
        )
    }

    if FileManager.default.fileExists(atPath: outputPDF) {
        do {
            try FileManager.default.removeItem(atPath: outputPDF)
        } catch {
            return copySearchablePDFFallback(
                inputPDF: inputPDF,
                outputPDF: outputPDF,
                warnings: ["failed_to_prepare_output_pdf"]
            )
        }
    }

    guard let outputURL = CFURLCreateWithFileSystemPath(nil, outputPDF as CFString, .cfurlposixPathStyle, false),
          let context = CGContext(outputURL, mediaBox: nil, nil)
    else {
        return copySearchablePDFFallback(
            inputPDF: inputPDF,
            outputPDF: outputPDF,
            warnings: ["failed_to_create_output_pdf_context"]
        )
    }

    var warnings: [String] = []
    for artifact in artifacts {
        let pageNumber = artifact.index + 1
        guard let page = source.page(at: pageNumber) else {
            warnings.append("missing_page_for_text_layer_\(pageNumber)")
            continue
        }

        var mediaBox = page.getBoxRect(.mediaBox)
        if mediaBox.isEmpty {
            mediaBox = artifact.pageBounds
        }

        context.beginPDFPage([
            kCGPDFContextMediaBox: mediaBox,
        ] as CFDictionary)
        context.drawPDFPage(page)

        for block in artifact.ocrPage.blocks {
            let trimmed = block.text.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.isEmpty {
                continue
            }
            let rect = pdfRect(for: block, artifact: artifact)
            drawInvisibleText(trimmed, in: rect, context: context)
        }

        context.endPDFPage()
    }

    context.closePDF()
    if !FileManager.default.fileExists(atPath: outputPDF) {
        warnings.append("searchable_pdf_text_layer_write_missing")
        return copySearchablePDFFallback(inputPDF: inputPDF, outputPDF: outputPDF, warnings: warnings)
    }
    warnings.append("searchable_pdf_method=coregraphics-invisible-text-layer")
    return ("coregraphics-invisible-text-layer", warnings)

}

func writeDocument(_ document: OCRDocument, to path: String) throws {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    var payload = try encoder.encode(document)
    payload.append(Data("\n".utf8))
    try writeFile(path: path, content: payload)
}

func renderText(pages: [OCRPage]) -> String {
    return pages.map { $0.text }.joined(separator: "\n\n")
}

func renderMarkdown(pages: [OCRPage]) -> String {
    var lines: [String] = []
    for page in pages {
        lines.append("## Page \(page.page)")
        for block in page.blocks.sorted(by: { $0.readingOrder < $1.readingOrder }) {
            let text = block.text.trimmingCharacters(in: .whitespacesAndNewlines)
            if text.isEmpty {
                continue
            }
            switch block.blockType {
            case "heading":
                lines.append("### \(text)")
            case "code":
                lines.append("```text")
                lines.append(text)
                lines.append("```")
            case "caption":
                lines.append("*\(text)*")
            default:
                lines.append(text)
            }
        }
        lines.append("")
    }
    return lines.joined(separator: "\n")
}

func run() throws {
    let totalStart = Date()
    let inputData = FileHandle.standardInput.readDataToEndOfFile()
    guard !inputData.isEmpty else {
        throw ProviderError.invalidRequest
    }

    let request = try JSONDecoder().decode(Request.self, from: inputData)
    guard request.input_pdf.lowercased().hasSuffix(".pdf") else {
        throw ProviderError.invalidInputPDF(request.input_pdf)
    }
    guard FileManager.default.fileExists(atPath: request.input_pdf) else {
        throw ProviderError.invalidInputPDF(request.input_pdf)
    }

    try FileManager.default.createDirectory(atPath: request.output_dir, withIntermediateDirectories: true)

    guard let document = PDFDocument(url: URL(fileURLWithPath: request.input_pdf)) else {
        throw ProviderError.failedToLoadPDF(request.input_pdf)
    }

    let renderScaleValue = renderScale(for: request.profile)
    let ocrStart = Date()
    var artifacts: [PageArtifact] = []
    artifacts.reserveCapacity(document.pageCount)

    var warnings: [String] = []
    if request.max_workers > 1 {
        warnings.append("max_workers_not_applied_yet_in_swift_provider")
    }

    for idx in 0..<document.pageCount {
        guard let page = document.page(at: idx) else {
            warnings.append("missing_pdf_page_\(idx + 1)")
            continue
        }
        let artifact = try processPage(page: page, pageNumber: idx + 1, scale: renderScaleValue)
        artifacts.append(artifact)
    }
    let ocrSeconds = Date().timeIntervalSince(ocrStart)

    let pages = artifacts.map { $0.ocrPage }
    let ocrDocument = OCRDocument(engine: "vision-swift", sourcePDF: request.input_pdf, pages: pages)

    let pagesJSON = (request.output_dir as NSString).appendingPathComponent("pages.json")
    let textPath = (request.output_dir as NSString).appendingPathComponent("document.txt")
    let markdownPath = (request.output_dir as NSString).appendingPathComponent("document.md")
    let searchablePDF = (request.output_dir as NSString).appendingPathComponent("searchable.pdf")

    let serializeStart = Date()
    try writeDocument(ocrDocument, to: pagesJSON)
    try writeFile(path: textPath, content: Data((renderText(pages: pages) + "\n").utf8))
    try writeFile(path: markdownPath, content: Data((renderMarkdown(pages: pages).trimmingCharacters(in: .whitespacesAndNewlines) + "\n").utf8))
    let serializeSeconds = Date().timeIntervalSince(serializeStart)

    let searchableStart = Date()
    let (_, searchableWarnings) = buildSearchablePDF(inputPDF: request.input_pdf, outputPDF: searchablePDF, artifacts: artifacts)
    warnings.append(contentsOf: searchableWarnings)
    let searchableSeconds = Date().timeIntervalSince(searchableStart)

    let totalSeconds = Date().timeIntervalSince(totalStart)

    let result = Result(
        searchable_pdf: searchablePDF,
        pages_json: pagesJSON,
        text_path: textPath,
        markdown_path: markdownPath,
        stage_timings: [
            "vision_ocr_seconds": ocrSeconds,
            "serialization_seconds": serializeSeconds,
            "searchable_pdf_seconds": searchableSeconds,
            "provider_total_seconds": totalSeconds
        ],
        warnings: warnings
    )

    let output = try JSONEncoder().encode(result)
    FileHandle.standardOutput.write(output)
}

do {
    try run()
} catch {
    let message = "vision-provider error: \(error)\n"
    FileHandle.standardError.write(Data(message.utf8))
    exit(1)
}
