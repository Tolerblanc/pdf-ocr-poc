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
    let shard_index: Int?
    let shard_total: Int?
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

struct ProgressEvent: Encodable {
    let phase: String
    let stage: String
    let current_page: Int?
    let completed_pages: Int?
    let total_pages: Int?
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

struct PageChunk {
    let startIndex: Int
    let pageCount: Int
    let shardIndex: Int?
    let shardTotal: Int?
}

struct PageStageTiming: Encodable {
    let page: Int
    let renderWorker: String
    let recognizeWorker: String
    let renderSeconds: Double
    let queueWaitSeconds: Double
    let recognizeSeconds: Double
    let postprocessSeconds: Double
    let totalSeconds: Double
}

struct WorkerStageSummary: Encodable {
    let worker: String
    let pages: Int
    let busySeconds: Double
}

struct OCRStageProfile: Encodable {
    let mode: String
    let requestedMaxWorkers: Int
    let effectiveMaxWorkers: Int
    let renderWorkers: Int
    let recognizeWorkers: Int
    let renderQueueCapacity: Int
    let pageCount: Int
    let wallSeconds: Double
    let renderTotalSeconds: Double
    let recognizeTotalSeconds: Double
    let postprocessTotalSeconds: Double
    let queueWaitTotalSeconds: Double
    let pageTotalSeconds: Double
    let renderAverageSeconds: Double
    let recognizeAverageSeconds: Double
    let postprocessAverageSeconds: Double
    let pageAverageSeconds: Double
    let longestPageSeconds: Double
    let maxActivePages: Int
    let maxActiveRenderWorkers: Int
    let maxActiveRecognizeWorkers: Int
    let maxQueuedRenderedPages: Int
    let renderEffectiveParallelism: Double
    let recognizeEffectiveParallelism: Double
    let pageEffectiveParallelism: Double
    let renderWorkerSummaries: [WorkerStageSummary]
    let recognizeWorkerSummaries: [WorkerStageSummary]
    let pages: [PageStageTiming]
}

struct OCRStageOutput {
    let artifacts: [PageArtifact]
    let warnings: [String]
    let profile: OCRStageProfile
}

struct RenderedPageArtifact {
    let index: Int
    let pageBounds: CGRect
    let imageWidth: Int
    let imageHeight: Int
    let cgImage: CGImage
    let assignedAt: Date
    let renderedAt: Date
}

struct MutablePageStageTiming {
    let page: Int
    var renderWorker: String = ""
    var recognizeWorker: String = ""
    var renderSeconds: Double = 0
    var queueWaitSeconds: Double = 0
    var recognizeSeconds: Double = 0
    var postprocessSeconds: Double = 0
    var totalSeconds: Double = 0
}

struct MutableWorkerStageSummary {
    var pages: Int = 0
    var busySeconds: Double = 0
}

final class OCRStageProfiler {
    private let lock = NSLock()
    private var pageTimings: [Int: MutablePageStageTiming] = [:]
    private var renderWorkers: [String: MutableWorkerStageSummary] = [:]
    private var recognizeWorkers: [String: MutableWorkerStageSummary] = [:]
    private var renderTotalSeconds: Double = 0
    private var recognizeTotalSeconds: Double = 0
    private var postprocessTotalSeconds: Double = 0
    private var queueWaitTotalSeconds: Double = 0
    private var pageTotalSeconds: Double = 0
    private var longestPageSeconds: Double = 0
    private var activePages = 0
    private var activeRenderWorkers = 0
    private var activeRecognizeWorkers = 0
    private var maxActivePages = 0
    private var maxActiveRenderWorkers = 0
    private var maxActiveRecognizeWorkers = 0
    private var maxQueuedRenderedPages = 0

    private func ensurePageTiming(_ page: Int) {
        if pageTimings[page] == nil {
            pageTimings[page] = MutablePageStageTiming(page: page)
        }
    }

    func pageStarted(page: Int) {
        lock.withLock {
            activePages += 1
            maxActivePages = max(maxActivePages, activePages)
            ensurePageTiming(page)
        }
    }

    func pageAborted(page: Int) {
        lock.withLock {
            activePages = max(0, activePages - 1)
            ensurePageTiming(page)
        }
    }

    func renderStarted() {
        lock.withLock {
            activeRenderWorkers += 1
            maxActiveRenderWorkers = max(maxActiveRenderWorkers, activeRenderWorkers)
        }
    }

    func renderCompleted(page: Int, worker: String, seconds: Double) {
        lock.withLock {
            activeRenderWorkers = max(0, activeRenderWorkers - 1)
            renderTotalSeconds += seconds
            var pageTiming = pageTimings[page] ?? MutablePageStageTiming(page: page)
            pageTiming.renderWorker = worker
            pageTiming.renderSeconds = seconds
            pageTimings[page] = pageTiming

            var summary = renderWorkers[worker] ?? MutableWorkerStageSummary()
            summary.pages += 1
            summary.busySeconds += seconds
            renderWorkers[worker] = summary
        }
    }

    func renderFailed() {
        lock.withLock {
            activeRenderWorkers = max(0, activeRenderWorkers - 1)
        }
    }

    func recognizeStarted() {
        lock.withLock {
            activeRecognizeWorkers += 1
            maxActiveRecognizeWorkers = max(maxActiveRecognizeWorkers, activeRecognizeWorkers)
        }
    }

    func recognizeCompleted(page: Int, worker: String, queueWaitSeconds: Double, recognizeSeconds: Double, postprocessSeconds: Double, totalSeconds: Double) {
        lock.withLock {
            activeRecognizeWorkers = max(0, activeRecognizeWorkers - 1)
            recognizeTotalSeconds += recognizeSeconds
            postprocessTotalSeconds += postprocessSeconds
            queueWaitTotalSeconds += queueWaitSeconds
            pageTotalSeconds += totalSeconds
            longestPageSeconds = max(longestPageSeconds, totalSeconds)
            activePages = max(0, activePages - 1)

            var pageTiming = pageTimings[page] ?? MutablePageStageTiming(page: page)
            pageTiming.recognizeWorker = worker
            pageTiming.queueWaitSeconds = queueWaitSeconds
            pageTiming.recognizeSeconds = recognizeSeconds
            pageTiming.postprocessSeconds = postprocessSeconds
            pageTiming.totalSeconds = totalSeconds
            pageTimings[page] = pageTiming

            var summary = recognizeWorkers[worker] ?? MutableWorkerStageSummary()
            summary.pages += 1
            summary.busySeconds += recognizeSeconds + postprocessSeconds
            recognizeWorkers[worker] = summary
        }
    }

    func recognizeFailed(page: Int) {
        lock.withLock {
            activeRecognizeWorkers = max(0, activeRecognizeWorkers - 1)
            activePages = max(0, activePages - 1)
            ensurePageTiming(page)
        }
    }

    func buildProfile(
        mode: String,
        requestedMaxWorkers: Int,
        effectiveMaxWorkers: Int,
        renderWorkers: Int,
        recognizeWorkers: Int,
        renderQueueCapacity: Int,
        pageCount: Int,
        wallSeconds: Double
    ) -> OCRStageProfile {
        let state = lock.withLock {
            (
                pageTimings: pageTimings,
                renderWorkers: self.renderWorkers,
                recognizeWorkers: self.recognizeWorkers,
                renderTotalSeconds: renderTotalSeconds,
                recognizeTotalSeconds: recognizeTotalSeconds,
                postprocessTotalSeconds: postprocessTotalSeconds,
                queueWaitTotalSeconds: queueWaitTotalSeconds,
                pageTotalSeconds: pageTotalSeconds,
                longestPageSeconds: longestPageSeconds,
                maxActivePages: maxActivePages,
                maxActiveRenderWorkers: maxActiveRenderWorkers,
                maxActiveRecognizeWorkers: maxActiveRecognizeWorkers,
                maxQueuedRenderedPages: maxQueuedRenderedPages
            )
        }

        let orderedPages = state.pageTimings.keys.sorted().compactMap { page -> PageStageTiming? in
            guard let timing = state.pageTimings[page] else {
                return nil
            }
            return PageStageTiming(
                page: page,
                renderWorker: timing.renderWorker,
                recognizeWorker: timing.recognizeWorker,
                renderSeconds: timing.renderSeconds,
                queueWaitSeconds: timing.queueWaitSeconds,
                recognizeSeconds: timing.recognizeSeconds,
                postprocessSeconds: timing.postprocessSeconds,
                totalSeconds: timing.totalSeconds
            )
        }
        let renderAverageSeconds = orderedPages.isEmpty ? 0 : state.renderTotalSeconds / Double(orderedPages.count)
        let recognizeAverageSeconds = orderedPages.isEmpty ? 0 : state.recognizeTotalSeconds / Double(orderedPages.count)
        let postprocessAverageSeconds = orderedPages.isEmpty ? 0 : state.postprocessTotalSeconds / Double(orderedPages.count)
        let pageAverageSeconds = orderedPages.isEmpty ? 0 : state.pageTotalSeconds / Double(orderedPages.count)
        let safeWall = max(wallSeconds, 0.000001)

        return OCRStageProfile(
            mode: mode,
            requestedMaxWorkers: requestedMaxWorkers,
            effectiveMaxWorkers: effectiveMaxWorkers,
            renderWorkers: renderWorkers,
            recognizeWorkers: recognizeWorkers,
            renderQueueCapacity: renderQueueCapacity,
            pageCount: pageCount,
            wallSeconds: wallSeconds,
            renderTotalSeconds: state.renderTotalSeconds,
            recognizeTotalSeconds: state.recognizeTotalSeconds,
            postprocessTotalSeconds: state.postprocessTotalSeconds,
            queueWaitTotalSeconds: state.queueWaitTotalSeconds,
            pageTotalSeconds: state.pageTotalSeconds,
            renderAverageSeconds: renderAverageSeconds,
            recognizeAverageSeconds: recognizeAverageSeconds,
            postprocessAverageSeconds: postprocessAverageSeconds,
            pageAverageSeconds: pageAverageSeconds,
            longestPageSeconds: state.longestPageSeconds,
            maxActivePages: state.maxActivePages,
            maxActiveRenderWorkers: state.maxActiveRenderWorkers,
            maxActiveRecognizeWorkers: state.maxActiveRecognizeWorkers,
            maxQueuedRenderedPages: state.maxQueuedRenderedPages,
            renderEffectiveParallelism: state.renderTotalSeconds / safeWall,
            recognizeEffectiveParallelism: state.recognizeTotalSeconds / safeWall,
            pageEffectiveParallelism: state.pageTotalSeconds / safeWall,
            renderWorkerSummaries: state.renderWorkers.keys.sorted().map { worker in
                let summary = state.renderWorkers[worker] ?? MutableWorkerStageSummary()
                return WorkerStageSummary(worker: worker, pages: summary.pages, busySeconds: summary.busySeconds)
            },
            recognizeWorkerSummaries: state.recognizeWorkers.keys.sorted().map { worker in
                let summary = state.recognizeWorkers[worker] ?? MutableWorkerStageSummary()
                return WorkerStageSummary(worker: worker, pages: summary.pages, busySeconds: summary.busySeconds)
            },
            pages: orderedPages
        )
    }
}

enum ProviderError: Error {
    case invalidRequest
    case invalidInputPDF(String)
    case failedToLoadPDF(String)
    case failedToRenderPage(Int)
    case failedToRecognizePage(Int)
    case invalidShardConfiguration(String)
}

extension NSLock {
    func withLock<T>(_ body: () throws -> T) rethrows -> T {
        lock()
        defer { unlock() }
        return try body()
    }
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

let progressWriteQueue = DispatchQueue(label: "vision-provider.progress")

func emitProgress(_ event: ProgressEvent) {
    progressWriteQueue.sync {
        let encoder = JSONEncoder()
        guard let payload = try? encoder.encode(event) else {
            return
        }
        FileHandle.standardError.write(Data("OCRPOC_PROGRESS ".utf8))
        FileHandle.standardError.write(payload)
        FileHandle.standardError.write(Data("\n".utf8))
    }
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

func renderPageArtifact(
    page: PDFPage,
    pageNumber: Int,
    scale: CGFloat,
    workerID: String,
    assignedAt: Date,
    profiler: OCRStageProfiler
) throws -> RenderedPageArtifact {
    profiler.renderStarted()
    let renderStart = Date()
    let cgImage = try renderPageImage(page: page, scale: scale, pageNumber: pageNumber)
    let renderSeconds = Date().timeIntervalSince(renderStart)
    profiler.renderCompleted(page: pageNumber, worker: workerID, seconds: renderSeconds)

    return RenderedPageArtifact(
        index: pageNumber - 1,
        pageBounds: page.bounds(for: .mediaBox),
        imageWidth: cgImage.width,
        imageHeight: cgImage.height,
        cgImage: cgImage,
        assignedAt: assignedAt,
        renderedAt: Date()
    )
}

func recognizeRenderedPage(
    _ rendered: RenderedPageArtifact,
    pageNumber: Int,
    workerID: String,
    profiler: OCRStageProfiler
) throws -> PageArtifact {
    profiler.recognizeStarted()
    let queueWaitSeconds = max(0, Date().timeIntervalSince(rendered.renderedAt))
    let recognizeStart = Date()
    let lines = try recognizeLines(cgImage: rendered.cgImage, pageNumber: pageNumber)
    let recognizeSeconds = Date().timeIntervalSince(recognizeStart)

    let postprocessStart = Date()

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
    let postprocessSeconds = Date().timeIntervalSince(postprocessStart)
    let totalSeconds = Date().timeIntervalSince(rendered.assignedAt)
    profiler.recognizeCompleted(
        page: pageNumber,
        worker: workerID,
        queueWaitSeconds: queueWaitSeconds,
        recognizeSeconds: recognizeSeconds,
        postprocessSeconds: postprocessSeconds,
        totalSeconds: totalSeconds
    )

    let ocrPage = OCRPage(
        page: pageNumber,
        width: rendered.imageWidth,
        height: rendered.imageHeight,
        isBlank: pageText.isEmpty,
        text: pageText,
        blocks: blocks
    )

    return PageArtifact(
        index: rendered.index,
        pageBounds: rendered.pageBounds,
        imageWidth: rendered.imageWidth,
        imageHeight: rendered.imageHeight,
        ocrPage: ocrPage
    )
}

func effectiveOCRWorkers(requested: Int, totalPages: Int) -> Int {
    if totalPages < 1 {
        return 1
    }
    let bounded = max(1, requested)
    return min(bounded, totalPages)
}

func tunedVisionWorkers(requested: Int, totalPages: Int) -> Int {
    let effectiveWorkers = effectiveOCRWorkers(requested: requested, totalPages: totalPages)
    if effectiveWorkers <= 1 {
        return effectiveWorkers
    }
    return min(2, effectiveWorkers)
}

func resolvePageChunk(pageCount: Int, shardIndex: Int?, shardTotal: Int?) throws -> PageChunk {
    let index = shardIndex ?? 0
    let total = shardTotal ?? 0
    if index == 0 && total == 0 {
        return PageChunk(startIndex: 0, pageCount: pageCount, shardIndex: nil, shardTotal: nil)
    }
    guard index >= 1, total >= 1, index <= total else {
        throw ProviderError.invalidShardConfiguration("invalid shard request index=\(index) total=\(total)")
    }
    if pageCount > 0 && total > pageCount {
        throw ProviderError.invalidShardConfiguration("shard_total exceeds page_count total=\(total) page_count=\(pageCount)")
    }
    let base = pageCount / total
    let remainder = pageCount % total
    let extraBefore = min(index - 1, remainder)
    let startIndex = ((index - 1) * base) + extraBefore
    let chunkPageCount = base + (index <= remainder ? 1 : 0)
    return PageChunk(startIndex: startIndex, pageCount: chunkPageCount, shardIndex: index, shardTotal: total)
}

func processPageInline(
    page: PDFPage,
    pageNumber: Int,
    scale: CGFloat,
    workerID: String,
    assignedAt: Date,
    profiler: OCRStageProfiler
) throws -> PageArtifact {
    let rendered: RenderedPageArtifact
    do {
        rendered = try renderPageArtifact(
            page: page,
            pageNumber: pageNumber,
            scale: scale,
            workerID: workerID,
            assignedAt: assignedAt,
            profiler: profiler
        )
    } catch {
        profiler.renderFailed()
        profiler.pageAborted(page: pageNumber)
        throw error
    }

    do {
        return try recognizeRenderedPage(rendered, pageNumber: pageNumber, workerID: workerID, profiler: profiler)
    } catch {
        profiler.recognizeFailed(page: pageNumber)
        throw error
    }
}

func processDocumentPages(inputPDF: String, pageChunk: PageChunk, scale: CGFloat, maxWorkers: Int) throws -> OCRStageOutput {
    let pageCount = pageChunk.pageCount
    if pageCount == 0 {
        let profile = OCRStageProfiler().buildProfile(
            mode: "capped_page_workers",
            requestedMaxWorkers: maxWorkers,
            effectiveMaxWorkers: 0,
            renderWorkers: 0,
            recognizeWorkers: 0,
            renderQueueCapacity: 0,
            pageCount: 0,
            wallSeconds: 0
        )
        return OCRStageOutput(artifacts: [], warnings: [], profile: profile)
    }

    let workerCount = effectiveOCRWorkers(requested: maxWorkers, totalPages: pageCount)
    let visionWorkers = tunedVisionWorkers(requested: maxWorkers, totalPages: pageCount)
    let lock = NSLock()
    let profiler = OCRStageProfiler()
    var nextIndex = 0
    var completedPages = 0
    var firstError: Error?
    var warnings: [String] = []
    var artifacts = Array<PageArtifact?>(repeating: nil, count: pageCount)
    let inputURL = URL(fileURLWithPath: inputPDF)
    let stageStart = Date()
    if visionWorkers < workerCount {
        warnings.append("vision_recognize_workers_capped=\(visionWorkers)")
    }

    emitProgress(
        ProgressEvent(
            phase: "document_started",
            stage: "vision_ocr",
            current_page: nil,
            completed_pages: 0,
            total_pages: pageCount
        )
    )

    let group = DispatchGroup()
    for workerIndex in 0..<visionWorkers {
        group.enter()
        let workerID = String(format: "ocr-%02d", workerIndex + 1)
        DispatchQueue.global(qos: .userInitiated).async {
            defer { group.leave() }

            guard let workerDocument = PDFDocument(url: inputURL) else {
                lock.withLock {
                    if firstError == nil {
                        firstError = ProviderError.failedToLoadPDF(inputPDF)
                    }
                }
                return
            }

            while true {
                let pageIndex: Int? = lock.withLock {
                    if firstError != nil || nextIndex >= pageCount {
                        return nil
                    }
                    let assigned = nextIndex
                    nextIndex += 1
                    return assigned
                }
                guard let idx = pageIndex else {
                    return
                }
                let absoluteIndex = pageChunk.startIndex + idx
                let pageNumber = absoluteIndex + 1

                let assignedAt = Date()
                emitProgress(
                    ProgressEvent(
                        phase: "page_started",
                        stage: "vision_ocr",
                        current_page: pageNumber,
                        completed_pages: lock.withLock { completedPages },
                        total_pages: pageCount
                    )
                )
                profiler.pageStarted(page: pageNumber)

                autoreleasepool {
                    guard let page = workerDocument.page(at: absoluteIndex) else {
                        lock.withLock {
                            warnings.append("missing_pdf_page_\(pageNumber)")
                        }
                        profiler.pageAborted(page: pageNumber)
                        return
                    }

                    do {
                        let artifact = try processPageInline(
                            page: page,
                            pageNumber: pageNumber,
                            scale: scale,
                            workerID: workerID,
                            assignedAt: assignedAt,
                            profiler: profiler
                        )
                        let completed = lock.withLock { () -> Int in
                            artifacts[idx] = artifact
                            completedPages += 1
                            return completedPages
                        }
                        emitProgress(
                            ProgressEvent(
                                phase: "page_done",
                                stage: "vision_ocr",
                                current_page: pageNumber,
                                completed_pages: completed,
                                total_pages: pageCount
                            )
                        )
                    } catch {
                        lock.withLock {
                            if firstError == nil {
                                firstError = error
                            }
                        }
                        return
                    }
                }
            }
        }
    }

    group.wait()

    if let error = firstError {
        throw error
    }

    let orderedArtifacts = artifacts.compactMap { $0 }
    emitProgress(
        ProgressEvent(
            phase: "document_done",
            stage: "vision_ocr",
            current_page: nil,
            completed_pages: orderedArtifacts.count,
            total_pages: pageCount
        )
    )
    let wallSeconds = Date().timeIntervalSince(stageStart)
    let profile = profiler.buildProfile(
        mode: "capped_page_workers",
        requestedMaxWorkers: maxWorkers,
        effectiveMaxWorkers: workerCount,
        renderWorkers: visionWorkers,
        recognizeWorkers: visionWorkers,
        renderQueueCapacity: 0,
        pageCount: pageCount,
        wallSeconds: wallSeconds
    )
    return OCRStageOutput(artifacts: orderedArtifacts, warnings: warnings, profile: profile)
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

func writeOCRStageProfile(_ profile: OCRStageProfile, to path: String) throws {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    encoder.keyEncodingStrategy = .convertToSnakeCase
    var payload = try encoder.encode(profile)
    payload.append(Data("\n".utf8))
    try writeFile(path: path, content: payload)
}

func ocrStageTimingMap(_ profile: OCRStageProfile) -> [String: Double] {
    return [
        "vision_ocr_seconds": profile.wallSeconds,
        "vision_ocr_render_seconds_total": profile.renderTotalSeconds,
        "vision_ocr_recognize_seconds_total": profile.recognizeTotalSeconds,
        "vision_ocr_postprocess_seconds_total": profile.postprocessTotalSeconds,
        "vision_ocr_queue_wait_seconds_total": profile.queueWaitTotalSeconds,
        "vision_ocr_page_seconds_total": profile.pageTotalSeconds,
        "vision_ocr_render_average_seconds": profile.renderAverageSeconds,
        "vision_ocr_recognize_average_seconds": profile.recognizeAverageSeconds,
        "vision_ocr_postprocess_average_seconds": profile.postprocessAverageSeconds,
        "vision_ocr_page_average_seconds": profile.pageAverageSeconds,
        "vision_ocr_longest_page_seconds": profile.longestPageSeconds,
        "vision_ocr_render_workers": Double(profile.renderWorkers),
        "vision_ocr_recognize_workers": Double(profile.recognizeWorkers),
        "vision_ocr_render_queue_capacity": Double(profile.renderQueueCapacity),
        "vision_ocr_max_active_pages": Double(profile.maxActivePages),
        "vision_ocr_max_active_render_workers": Double(profile.maxActiveRenderWorkers),
        "vision_ocr_max_active_recognize_workers": Double(profile.maxActiveRecognizeWorkers),
        "vision_ocr_max_queued_rendered_pages": Double(profile.maxQueuedRenderedPages),
        "vision_ocr_render_effective_parallelism": profile.renderEffectiveParallelism,
        "vision_ocr_recognize_effective_parallelism": profile.recognizeEffectiveParallelism,
        "vision_ocr_page_effective_parallelism": profile.pageEffectiveParallelism,
    ]
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
    var warnings: [String] = []
    let pageChunk = try resolvePageChunk(
        pageCount: document.pageCount,
        shardIndex: request.shard_index,
        shardTotal: request.shard_total
    )
    let ocrStage = try processDocumentPages(
        inputPDF: request.input_pdf,
        pageChunk: pageChunk,
        scale: renderScaleValue,
        maxWorkers: request.max_workers
    )
    warnings.append(contentsOf: ocrStage.warnings)
    let ocrSeconds = Date().timeIntervalSince(ocrStart)

    let artifacts = ocrStage.artifacts
    let pages = artifacts.map { $0.ocrPage }
    let ocrDocument = OCRDocument(engine: "vision-swift", sourcePDF: request.input_pdf, pages: pages)

    let pagesJSON = (request.output_dir as NSString).appendingPathComponent("pages.json")
    let textPath = (request.output_dir as NSString).appendingPathComponent("document.txt")
    let markdownPath = (request.output_dir as NSString).appendingPathComponent("document.md")
    let searchablePDF = (request.output_dir as NSString).appendingPathComponent("searchable.pdf")
    let ocrStageProfilePath = (request.output_dir as NSString).appendingPathComponent("ocr_stage_profile.json")
    try writeOCRStageProfile(ocrStage.profile, to: ocrStageProfilePath)

    let serializeStart = Date()
    emitProgress(
        ProgressEvent(
            phase: "stage_started",
            stage: "serialization",
            current_page: nil,
            completed_pages: nil,
            total_pages: pageChunk.pageCount
        )
    )
    try writeDocument(ocrDocument, to: pagesJSON)
    try writeFile(path: textPath, content: Data((renderText(pages: pages) + "\n").utf8))
    try writeFile(path: markdownPath, content: Data((renderMarkdown(pages: pages).trimmingCharacters(in: .whitespacesAndNewlines) + "\n").utf8))
    let serializeSeconds = Date().timeIntervalSince(serializeStart)

    let searchableStart = Date()
    emitProgress(
        ProgressEvent(
            phase: "stage_started",
            stage: "searchable_pdf",
            current_page: nil,
            completed_pages: nil,
            total_pages: pageChunk.pageCount
        )
    )
    let (_, searchableWarnings) = buildSearchablePDF(inputPDF: request.input_pdf, outputPDF: searchablePDF, artifacts: artifacts)
    warnings.append(contentsOf: searchableWarnings)
    let searchableSeconds = Date().timeIntervalSince(searchableStart)

    let totalSeconds = Date().timeIntervalSince(totalStart)
    var stageTimings = ocrStageTimingMap(ocrStage.profile)
    stageTimings["vision_ocr_seconds"] = ocrSeconds
    stageTimings["serialization_seconds"] = serializeSeconds
    stageTimings["searchable_pdf_seconds"] = searchableSeconds
    stageTimings["provider_total_seconds"] = totalSeconds

    let result = Result(
        searchable_pdf: searchablePDF,
        pages_json: pagesJSON,
        text_path: textPath,
        markdown_path: markdownPath,
        stage_timings: stageTimings,
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
