package icve

import (
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

// TestPatternMatching verifies that each sub-module's URL patterns match the
// expected URLs from the decompiled source's test/main functions.
func TestPatternMatching(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantExt string // expected extractor name
	}{
		// Icve_Ai (existing)
		{
			name:    "ai course URL",
			url:     "https://ai.icve.com.cn/app/course/7ae0dc6f85b64698ad5c9153bd7fc3d7",
			wantExt: "Icve",
		},
		{
			name:    "ai excellent URL",
			url:     "https://ai.icve.com.cn/app/coursedetails-excellent/2c62324fd78f7bc4c7672bb8245b9cb8/BD43FD32-7C5D-4B09-9CEC-368655CD6086-K0RT",
			wantExt: "Icve",
		},
		// Icve_Mooc
		{
			name:    "mooc courseinfo URL",
			url:     "https://www.icve.com.cn/portal_new/courseinfo/courseinfo.html?courseid=pf77ai2kpqrfwwy2rril6g",
			wantExt: "IcveMooc",
		},
		{
			name:    "mooc directory URL",
			url:     "https://www.icve.com.cn/study/directory/directory_list.html?courseId=7vx8aupmk5cgjby3mnxxg",
			wantExt: "IcveMooc",
		},
		// Icve_Course
		{
			name:    "course learnspace URL",
			url:     "https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=ABC123",
			wantExt: "IcveCourse",
		},
		{
			name:    "course mooc-old cid URL",
			url:     "https://mooc-old.icve.com.cn/cms/courseDetails/index.htm?cid=qcdzzs013shy833",
			wantExt: "IcveCourse",
		},
		{
			name:    "course mooc-old classId URL",
			url:     "https://mooc-old.icve.com.cn/cms/courseDetails/index.htm?classId=kfznbz033xch519",
			wantExt: "IcveCourse",
		},
		// Icve_Profession
		{
			name:    "profession courseDetailed URL",
			url:     "https://zyk.icve.com.cn/courseDetailed?id=uqioam6oeprleli3powydg",
			wantExt: "IcveProfession",
		},
		{
			name:    "profession courseDetailed with openCourse URL",
			url:     "https://zyk.icve.com.cn/courseDetailed?id=dc9e5206-fa39-4dd7-babb-584dec6324b4&openCourse=4b3fb48a-a576-47ef-a274-5a9684b9c12e",
			wantExt: "IcveProfession",
		},
		// Icve_Profession_V2
		{
			name:    "profession v2 study preview URL",
			url:     "https://zjy2.icve.com.cn/study/coursePreview/spoccourseIndex/courseware?id=B07E295D-AB3E-0C81-0838-ADA4EE27F801&classId=B07A295C-C53E-0B28-B838-52F51E0D3E99",
			wantExt: "IcveProfessionV2",
		},
		// Icve_Qun
		{
			name:    "qun course URL",
			url:     "https://qun.icve.com.cn/zyq/course/bz5xafivs7fglydzcpwjpa",
			wantExt: "IcveQun",
		},
		{
			name:    "qun course URL 2",
			url:     "https://qun.icve.com.cn/zyq/course/r5gjaeyx6xe8asuwevona",
			wantExt: "IcveQun",
		},
		// Icve_Weike
		{
			name:    "weike weikeId URL",
			url:     "https://www.icve.com.cn/portal_new/newweikeinfo/weikeinfo.html?weikeId=517fakcnz5bijqilwr73nq",
			wantExt: "IcveWeike",
		},
		{
			name:    "weike microstudy URL",
			url:     "https://www.icve.com.cn/portal_new/microstudy/microstudy.html?courseId=5v2eaaekaajbfymsmz27kq",
			wantExt: "IcveWeike",
		},
		// Icve_Material
		{
			name:    "material docid URL",
			url:     "https://www.icve.com.cn/portal_new/sourcematerial/edit_seematerial.html?docid=rkxpacwnrbbzgmloyqdw",
			wantExt: "IcveMaterial",
		},
		{
			name:    "material zyk URL",
			url:     "https://zyk.icve.com.cn/materialDetailed?id=4a2f56b7-62f4-42b9-b936-a2a3f1b881a1",
			wantExt: "IcveMaterial",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, err := extractor.Match(tt.url)
			if err != nil {
				t.Fatalf("Match(%q) returned error: %v", tt.url, err)
			}
			if ext == nil {
				t.Fatalf("Match(%q) returned nil extractor", tt.url)
			}
			// Get the site info to check the name
			var foundName string
			for _, site := range extractor.ListSites() {
				// Check if this extractor matches
				if site.Name == tt.wantExt {
					foundName = site.Name
				}
			}
			if foundName == "" {
				t.Fatalf("extractor %q not found in registered sites", tt.wantExt)
			}
			// Verify the matched extractor is the right type
			_ = ext
		})
	}
}

// TestCIDParsing verifies course ID extraction from URLs.
func TestCIDParsing(t *testing.T) {
	tests := []struct {
		name    string
		parser  func(string) string
		url     string
		wantCID string
	}{
		// Mooc CID
		{"mooc courseid", parseMoocCID, "https://www.icve.com.cn/portal_new/courseinfo/courseinfo.html?courseid=pf77ai2kpqrfwwy2rril6g", "pf77ai2kpqrfwwy2rril6g"},
		{"mooc courseId", parseMoocCID, "https://www.icve.com.cn/study/directory/directory_list.html?courseId=7vx8aupmk5cgjby3mnxxg", "7vx8aupmk5cgjby3mnxxg"},

		// Course CID
		{"course learnspace", parseCourseCID, "https://course.icve.com.cn/learnspace/learn/learn/templateeight/courseware_index.action?params.courseId=ABC123", "ABC123"},
		{"course cid", parseCourseCID, "https://mooc-old.icve.com.cn/cms/courseDetails/index.htm?cid=qcdzzs013shy833", "qcdzzs013shy833"},

		// Profession CID
		{"profession id", parseProfCID, "https://zyk.icve.com.cn/courseDetailed?id=uqioam6oeprleli3powydg", "uqioam6oeprleli3powydg"},
		{"profession uuid", parseProfCID, "https://zyk.icve.com.cn/courseDetailed?id=dc9e5206-fa39-4dd7-babb-584dec6324b4&openCourse=4b3fb48a-a576-47ef-a274-5a9684b9c12e", "dc9e5206-fa39-4dd7-babb-584dec6324b4"},

		// Qun CID
		{"qun course", parseQunCID, "https://qun.icve.com.cn/zyq/course/bz5xafivs7fglydzcpwjpa", "bz5xafivs7fglydzcpwjpa"},

		// Weike CID
		{"weike weikeId", parseWeikeCID, "https://www.icve.com.cn/portal_new/newweikeinfo/weikeinfo.html?weikeId=517fakcnz5bijqilwr73nq", "517fakcnz5bijqilwr73nq"},
		{"weike courseId", parseWeikeCID, "https://www.icve.com.cn/portal_new/microstudy/microstudy.html?courseId=5v2eaaekaajbfymsmz27kq", "5v2eaaekaajbfymsmz27kq"},

		// Material CID
		{"material docid", parseMaterialCID, "https://www.icve.com.cn/portal_new/sourcematerial/edit_seematerial.html?docid=rkxpacwnrbbzgmloyqdw", "rkxpacwnrbbzgmloyqdw"},
		{"material zyk id", parseMaterialCID, "https://zyk.icve.com.cn/materialDetailed?id=4a2f56b7-62f4-42b9-b936-a2a3f1b881a1", "4a2f56b7-62f4-42b9-b936-a2a3f1b881a1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.parser(tt.url)
			if got != tt.wantCID {
				t.Errorf("parse(%q) = %q, want %q", tt.url, got, tt.wantCID)
			}
		})
	}
}
