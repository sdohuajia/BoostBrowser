from PIL import Image
import sys
import os

def crop_to_5_2(input_path, output_path):
    if not os.path.exists(input_path):
        print(f"Error: {input_path} not found.")
        return
    img = Image.open(input_path)
    w, h = img.size
    target_ratio = 2.5 # 5/2
    
    current_ratio = w / h
    
    if current_ratio > target_ratio:
        # Too wide, crop left/right
        new_w = h * target_ratio
        left = (w - new_w) / 2
        img = img.crop((left, 0, left + new_w, h))
    else:
        # Too tall, crop top/bottom
        new_h = w / target_ratio
        top = (h - new_h) / 2
        img = img.crop((0, top, w, top + new_h))
    
    img.save(output_path)
    print(f"Cropped to 5:2 and saved to {output_path}")

if __name__ == "__main__":
    import glob
    # Find the latest generated base image in the brain folder (or just use the path I know)
    base_img = "base.png"
    crop_to_5_2(base_img, "github_banner.png")
