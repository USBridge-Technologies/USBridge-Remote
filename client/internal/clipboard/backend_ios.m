#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <UIKit/UIKit.h>
#include <string.h>
#include <stdlib.h>

// Custom pasteboard types used for the single-file round trip (see
// backend_ios.go's KindFile comment) — iOS has no generalized multi-file
// clipboard slot the way desktop OSes do, so this only interoperates
// reliably between USBridge instances, not with an arbitrary iOS app's own
// "copy file" action (which uses that app's own document UTI).
static NSString *const kUSBFileDataType = @"com.usbridge.client.file-data";
static NSString *const kUSBFileNameType = @"com.usbridge.client.file-name";

int clipboard_change_count(void) {
    @autoreleasepool {
        return (int)[UIPasteboard generalPasteboard].changeCount;
    }
}

char *clipboard_get_text(void) {
    @autoreleasepool {
        NSString *s = [UIPasteboard generalPasteboard].string;
        if (!s) {
            return NULL;
        }
        const char *utf8 = s.UTF8String;
        if (!utf8) {
            return NULL;
        }
        return strdup(utf8);
    }
}

int clipboard_set_text(const char *text) {
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:text];
        [UIPasteboard generalPasteboard].string = s;
        return 1;
    }
}

unsigned char *clipboard_get_image_png(size_t *outLen) {
    @autoreleasepool {
        *outLen = 0;
        UIPasteboard *pb = [UIPasteboard generalPasteboard];
        // "public.png" is also what UIPasteboard's own .image getter looks
        // for (per Apple's docs it recognizes kUTTypePNG/kUTTypeJPEG data),
        // so this one type covers both our own writes and anything another
        // app put there via pb.image = ....
        NSData *data = [pb dataForPasteboardType:@"public.png"];
        if (!data) {
            UIImage *img = pb.image;
            if (img) {
                data = UIImagePNGRepresentation(img);
            }
        }
        if (!data) {
            return NULL;
        }
        size_t len = data.length;
        unsigned char *buf = malloc(len);
        if (!buf) {
            return NULL;
        }
        memcpy(buf, data.bytes, len);
        *outLen = len;
        return buf;
    }
}

int clipboard_set_image_png(const unsigned char *data, size_t len) {
    @autoreleasepool {
        NSData *pngData = [NSData dataWithBytes:data length:len];
        NSDictionary *item = @{ @"public.png": pngData };
        [UIPasteboard generalPasteboard].items = @[item];
        return 1;
    }
}

int clipboard_get_file_count(void) {
    @autoreleasepool {
        NSData *data = [[UIPasteboard generalPasteboard] dataForPasteboardType:kUSBFileDataType];
        return data ? 1 : 0;
    }
}

char *clipboard_get_file_name(void) {
    @autoreleasepool {
        NSString *name = [[UIPasteboard generalPasteboard] valueForPasteboardType:kUSBFileNameType];
        if (!name) {
            name = @"file";
        }
        return strdup(name.UTF8String);
    }
}

unsigned char *clipboard_get_file_data(size_t *outLen) {
    @autoreleasepool {
        *outLen = 0;
        NSData *data = [[UIPasteboard generalPasteboard] dataForPasteboardType:kUSBFileDataType];
        if (!data) {
            return NULL;
        }
        size_t len = data.length;
        unsigned char *buf = malloc(len);
        if (!buf) {
            return NULL;
        }
        memcpy(buf, data.bytes, len);
        *outLen = len;
        return buf;
    }
}

int clipboard_set_file(const char *name, const unsigned char *data, size_t len) {
    @autoreleasepool {
        NSData *fileData = [NSData dataWithBytes:data length:len];
        NSString *fileName = [NSString stringWithUTF8String:name];
        NSDictionary *item = @{
            kUSBFileDataType: fileData,
            kUSBFileNameType: fileName,
        };
        [UIPasteboard generalPasteboard].items = @[item];
        return 1;
    }
}

#endif // TARGET_OS_IPHONE
