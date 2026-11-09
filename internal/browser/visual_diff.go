package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
)

// PixelDiffResult holds the output of a pixel-level image comparison.
type PixelDiffResult struct {
	DiffBox       *Box    `json:"diff_box,omitempty"`
	DiffPct       float64 `json:"diff_pct"`
	MaxColorDist  float64 `json:"max_color_dist"`
	ReferenceHash string  `json:"reference_hash"`
	Match         bool    `json:"match"`
}

// DOMDiffNode represents a single node-level difference between two DOM trees.
type DOMDiffNode struct {
	Path     string `json:"path"`
	Property string `json:"property"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// DOMDiffResult holds the output of a DOM tree structure comparison.
type DOMDiffResult struct {
	NodesChanged []DOMDiffNode `json:"nodes_changed"`
	Added        int           `json:"added"`
	Removed      int           `json:"removed"`
	PropsChanged int           `json:"props_changed"`
}

// PixelSample represents a single sampled pixel with its color distance.
type PixelSample struct {
	X        int     `json:"x"`
	Y        int     `json:"y"`
	R        uint8   `json:"r"`
	G        uint8   `json:"g"`
	B        uint8   `json:"b"`
	ColorDist float64 `json:"color_dist"`
}

// PixelDiff compares the current screenshot against a reference image (base64 PNG).
// If selector is provided, only the element region is captured.
// threshold is the fraction of pixels that must differ to be considered a mismatch (0.0-1.0).
func (b *Browser) PixelDiff(ctx context.Context, referenceBase64 string, selector string, threshold float64) (*PixelDiffResult, error) {
	var opts ScreenshotOptions
	if selector != "" {
		box, err := b.elementBox(ctx, selector)
		if err != nil {
			return nil, fmt.Errorf("element box: %w", err)
		}
		opts.Clip = &box
	}
	opts.Format = "png"

	currentPNG, err := b.Screenshot(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}

	currentHash := fmt.Sprintf("%x", sha256Sum(currentPNG))

	if referenceBase64 == "" {
		return &PixelDiffResult{
			DiffPct:       0,
			MaxColorDist:  0,
			ReferenceHash: currentHash,
			Match:         true,
		}, nil
	}

	refBytes, err := base64.StdEncoding.DecodeString(referenceBase64)
	if err != nil {
		return nil, fmt.Errorf("decode reference base64: %w", err)
	}

	refImg, err := png.Decode(bytes.NewReader(refBytes))
	if err != nil {
		return nil, fmt.Errorf("decode reference PNG: %w", err)
	}

	curImg, err := png.Decode(bytes.NewReader(currentPNG))
	if err != nil {
		return nil, fmt.Errorf("decode current PNG: %w", err)
	}

	return compareImages(refImg, curImg, currentHash, threshold), nil
}

// compareImages performs pixel-by-pixel comparison of two images.
func compareImages(ref, cur image.Image, currentHash string, threshold float64) *PixelDiffResult {
	refBounds := ref.Bounds()
	curBounds := cur.Bounds()

	// Different dimensions → entire image is different
	if refBounds.Dx() != curBounds.Dx() || refBounds.Dy() != curBounds.Dy() {
		return &PixelDiffResult{
			DiffBox: &Box{
				X:      0,
				Y:      0,
				Width:  float64(max(refBounds.Dx(), curBounds.Dx())),
				Height: float64(max(refBounds.Dy(), curBounds.Dy())),
				Scale:  1,
			},
			DiffPct:       1.0,
			MaxColorDist:  441.67,
			ReferenceHash: currentHash,
			Match:         false,
		}
	}

	width := refBounds.Dx()
	height := refBounds.Dy()
	totalPixels := width * height
	diffPixels := 0
	var maxDist float64
	var minX, minY, maxX, maxY int = width, height, 0, 0

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r1, g1, b1, _ := ref.At(refBounds.Min.X+x, refBounds.Min.Y+y).RGBA()
			r2, g2, b2, _ := cur.At(curBounds.Min.X+x, curBounds.Min.Y+y).RGBA()

			dr := math.Abs(float64(r1>>8) - float64(r2>>8))
			dg := math.Abs(float64(g1>>8) - float64(g2>>8))
			db := math.Abs(float64(b1>>8) - float64(b2>>8))
			dist := math.Sqrt(dr*dr + dg*dg + db*db)

			if dist > 0 {
				diffPixels++
				if dist > maxDist {
					maxDist = dist
				}
				if x < minX { minX = x }
				if y < minY { minY = y }
				if x > maxX { maxX = x }
				if y > maxY { maxY = y }
			}
		}
	}

	diffPct := float64(diffPixels) / float64(totalPixels)
	var diffBox *Box
	if diffPixels > 0 {
		diffBox = &Box{
			X:      float64(minX),
			Y:      float64(minY),
			Width:  float64(maxX - minX + 1),
			Height: float64(maxY - minY + 1),
			Scale:  1,
		}
	}

	return &PixelDiffResult{
		DiffBox:       diffBox,
		DiffPct:       diffPct,
		MaxColorDist:  maxDist,
		ReferenceHash: currentHash,
		Match:         diffPct <= threshold,
	}
}

// DiffDOMTree compares the current accessibility tree against a reference tree (JSON).
// The reference tree should be the JSON output from a previous get_accessibility_tree call.
func (b *Browser) DiffDOMTree(ctx context.Context, referenceTreeJSON json.RawMessage) (*DOMDiffResult, error) {
	currentTree, err := b.GetAccessibilityTree(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current AX tree: %w", err)
	}

	if len(referenceTreeJSON) == 0 || string(referenceTreeJSON) == "null" {
		return &DOMDiffResult{}, nil
	}

	refNodes, err := flattenAXTree(referenceTreeJSON)
	if err != nil {
		return nil, fmt.Errorf("parse reference AX tree: %w", err)
	}

	curNodes, err := flattenAXTree(currentTree)
	if err != nil {
		return nil, fmt.Errorf("parse current AX tree: %w", err)
	}

	return diffAXNodeMaps(refNodes, curNodes), nil
}

// axFlatNode is a flattened AX tree node with a structural path.
type axFlatNode struct {
	Path       string         `json:"path"`
	Role       string         `json:"role"`
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties"`
}

// flattenAXTree converts the nested AX tree JSON into a map of path → axFlatNode.
func flattenAXTree(treeJSON json.RawMessage) (map[string]axFlatNode, error) {
	var tree struct {
		Nodes []struct {
			NodeID     string `json:"nodeId"`
			ParentID   *string `json:"parentId"`
			Role       struct {
				Value string `json:"value"`
			} `json:"role"`
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
			Properties []struct {
				Name  string `json:"name"`
				Value struct {
					Value any `json:"value"`
				} `json:"value"`
			} `json:"properties"`
			ChildIDs []string `json:"childIds"`
		} `json:"nodes"`
	}

	if err := json.Unmarshal(treeJSON, &tree); err != nil {
		return nil, err
	}

	// Build parent → children index for path construction
	nodeByID := make(map[string]int)
	for i, n := range tree.Nodes {
		nodeByID[n.NodeID] = i
	}

	// Count siblings with same role per parent for path indexing
	roleCountByParent := make(map[string]map[string]int)
	for _, n := range tree.Nodes {
		parentKey := ""
		if n.ParentID != nil {
			parentKey = *n.ParentID
		}
		if roleCountByParent[parentKey] == nil {
			roleCountByParent[parentKey] = make(map[string]int)
		}
		roleCountByParent[parentKey][n.Role.Value]++
	}

	// Track current index per parent+role
	roleIndexByParent := make(map[string]map[string]int)

	result := make(map[string]axFlatNode)
	for _, n := range tree.Nodes {
		parentPath := ""
		if n.ParentID != nil {
			if parentNode, ok := result[*n.ParentID]; ok {
				parentPath = parentNode.Path
			}
		}

		role := n.Role.Value
		parentKey := ""
		if n.ParentID != nil {
			parentKey = *n.ParentID
		}

		if roleIndexByParent[parentKey] == nil {
			roleIndexByParent[parentKey] = make(map[string]int)
		}
		idx := roleIndexByParent[parentKey][role]
		roleIndexByParent[parentKey][role]++

		path := fmt.Sprintf("%s/%s[%d]", parentPath, role, idx)

		props := make(map[string]any)
		for _, p := range n.Properties {
			props[p.Name] = p.Value.Value
		}

		result[n.NodeID] = axFlatNode{
			Path:       path,
			Role:       role,
			Name:       n.Name.Value,
			Properties: props,
		}
	}

	return result, nil
}

// diffAXNodeMaps compares two flattened AX tree maps and produces a DOMDiffResult.
func diffAXNodeMaps(ref, cur map[string]axFlatNode) *DOMDiffResult {
	var result DOMDiffResult

	// Find removed nodes (in ref but not cur)
	for path, refNode := range ref {
		if _, ok := cur[path]; !ok {
			result.Removed++
			result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
				Path:     path,
				Property: "node",
				Expected: fmt.Sprintf("role=%s name=%s", refNode.Role, refNode.Name),
				Actual:   "",
			})
		}
	}

	// Find added nodes (in cur but not ref)
	for path, curNode := range cur {
		if _, ok := ref[path]; !ok {
			result.Added++
			result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
				Path:     path,
				Property: "node",
				Expected: "",
				Actual:   fmt.Sprintf("role=%s name=%s", curNode.Role, curNode.Name),
			})
		}
	}

	// Find changed nodes (same path, different properties)
	for path, refNode := range ref {
		curNode, ok := cur[path]
		if !ok {
			continue
		}

		if refNode.Role != curNode.Role {
			result.PropsChanged++
			result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
				Path:     path,
				Property: "role",
				Expected: refNode.Role,
				Actual:   curNode.Role,
			})
		}

		if refNode.Name != curNode.Name {
			result.PropsChanged++
			result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
				Path:     path,
				Property: "name",
				Expected: refNode.Name,
				Actual:   curNode.Name,
			})
		}

		// Compare key properties
		for propName, refVal := range refNode.Properties {
			curVal, ok := curNode.Properties[propName]
			if !ok {
				result.PropsChanged++
				result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
					Path:     path,
					Property: propName,
					Expected: fmt.Sprintf("%v", refVal),
					Actual:   "",
				})
				continue
			}
			if fmt.Sprintf("%v", refVal) != fmt.Sprintf("%v", curVal) {
				result.PropsChanged++
				result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
					Path:     path,
					Property: propName,
					Expected: fmt.Sprintf("%v", refVal),
					Actual:   fmt.Sprintf("%v", curVal),
				})
			}
		}

		// Check for properties only in current
		for propName := range curNode.Properties {
			if _, ok := refNode.Properties[propName]; !ok {
				result.PropsChanged++
				result.NodesChanged = append(result.NodesChanged, DOMDiffNode{
					Path:     path,
					Property: propName,
					Expected: "",
					Actual:   fmt.Sprintf("%v", curNode.Properties[propName]),
				})
			}
		}
	}

	return &result
}

// SamplePixels samples pixels at grid intervals within a region and computes
// their color distance from a reference color.
// If selector is provided, the region is the element's bounding box.
// grid is the number of sample points along each axis (e.g., 10 = 10x10 = 100 samples).
// refColor is a hex color string like "#FF0000".
func (b *Browser) SamplePixels(ctx context.Context, selector string, clip *Box, grid int, refColor string) ([]PixelSample, error) {
	var region Box
	if selector != "" {
		box, err := b.elementBox(ctx, selector)
		if err != nil {
			return nil, fmt.Errorf("element box: %w", err)
		}
		region = box
	} else if clip != nil {
		region = *clip
	} else {
		// Default to full viewport
		region = Box{X: 0, Y: 0, Width: 1280, Height: 720, Scale: 1}
	}

	if grid < 2 {
		grid = 10
	}

	pngBytes, err := b.Screenshot(ctx, ScreenshotOptions{Format: "png", Clip: &region})
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}

	refR, refG, refB := parseHexColor(refColor)

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var samples []PixelSample
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			px := bounds.Min.X + (gx * (w - 1) / (grid - 1))
			py := bounds.Min.Y + (gy * (h - 1) / (grid - 1))

			r, g, b, _ := img.At(px, py).RGBA()
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)

			dr := float64(r8) - float64(refR)
			dg := float64(g8) - float64(refG)
			db := float64(b8) - float64(refB)
			dist := math.Sqrt(dr*dr + dg*dg + db*db)

			samples = append(samples, PixelSample{
				X:         gx * (w - 1) / (grid - 1),
				Y:         gy * (h - 1) / (grid - 1),
				R:         r8,
				G:         g8,
				B:         b8,
				ColorDist: dist,
			})
		}
	}

	return samples, nil
}

// parseHexColor parses a hex color string like "#FF0000" into RGB components.
func parseHexColor(hex string) (uint8, uint8, uint8) {
	if len(hex) < 7 || hex[0] != '#' {
		return 0, 0, 0
	}
	var r, g, b uint8
	fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// sha256Sum returns the SHA-256 hash of data as a byte slice.
func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
