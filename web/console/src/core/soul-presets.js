import soulPresetManifest from "../../../../assets/config/souls/presets.json";
import catSoulTemplate from "../../../../assets/config/souls/cat.md?raw";
import dogSoulTemplate from "../../../../assets/config/souls/dog.md?raw";
import researchScholarSoulTemplate from "../../../../assets/config/souls/research_scholar.md?raw";
import softwareEngineerSoulTemplate from "../../../../assets/config/souls/software_engineer.md?raw";
import catFaceImage from "../assets/souls/cat/Faceset.png";
import catSpriteImage from "../assets/souls/cat/SpriteSheet.png";
import dogFaceImage from "../assets/souls/dog/Faceset.png";
import dogSpriteImage from "../assets/souls/dog/SpriteSheet.png";
import engineerFaceImage from "../assets/souls/software_engineer/Faceset.png";
import engineerSpriteImage from "../assets/souls/software_engineer/SpriteSheet.png";
import scholarFaceImage from "../assets/souls/research_scholar/Faceset.png";
import scholarSpriteImage from "../assets/souls/research_scholar/SpriteSheet.png";

function normalizeSoulDocument(raw) {
  const value = String(raw || "").replace(/\r\n/g, "\n").trim();
  return value ? `${value}\n` : "";
}

const SOUL_PRESET_CONTENT = {
  research_scholar: normalizeSoulDocument(researchScholarSoulTemplate),
  software_engineer: normalizeSoulDocument(softwareEngineerSoulTemplate),
  cat: normalizeSoulDocument(catSoulTemplate),
  dog: normalizeSoulDocument(dogSoulTemplate),
};

const SOUL_PRESET_VISUALS = {
  research_scholar: {
    icon: "PhCube",
    faceSrc: scholarFaceImage,
    spriteSrc: scholarSpriteImage,
    spriteFrames: 4,
    spriteFrameWidth: 32,
    spriteFrameHeight: 32,
    spriteScale: 2.5,
  },
  software_engineer: {
    icon: "PhGauge",
    faceSrc: engineerFaceImage,
    spriteSrc: engineerSpriteImage,
    spriteFrames: 4,
    spriteFrameWidth: 32,
    spriteFrameHeight: 32,
    spriteScale: 2.5,
  },
  cat: {
    icon: "PhFingerprint",
    faceSrc: catFaceImage,
    spriteSrc: catSpriteImage,
    spriteFrames: 2,
    spriteFrameWidth: 16,
    spriteFrameHeight: 16,
    spriteScale: 2.5,
  },
  dog: {
    icon: "PhUsers",
    faceSrc: dogFaceImage,
    spriteSrc: dogSpriteImage,
    spriteFrames: 2,
    spriteFrameWidth: 16,
    spriteFrameHeight: 16,
    spriteScale: 2.5,
  },
};

export const SOUL_PRESETS = soulPresetManifest.map((item) => ({
  id: item.id,
  titleKey: item.web_title_key,
  noteKey: item.web_note_key,
  content: SOUL_PRESET_CONTENT[item.id] || "",
  ...(SOUL_PRESET_VISUALS[item.id] || {}),
}));

export function findSoulPreset(id) {
  return SOUL_PRESETS.find((item) => item.id === id) || SOUL_PRESETS[0];
}
