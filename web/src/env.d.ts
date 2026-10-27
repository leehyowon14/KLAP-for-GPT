declare module "*.css";
declare module "*.svg" {
  const url: string;
  export default url;
}
declare module "*.txt" {
  const text: string;
  export default text;
}
