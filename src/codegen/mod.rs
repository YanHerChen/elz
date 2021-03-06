use crate::ast::*;
use crate::codegen::tag::CodegenTag;

pub mod formatter;
pub mod ir;
pub mod llvm;
mod tag;

pub struct CodeGenerator {}

impl CodeGenerator {
    pub fn new() -> CodeGenerator {
        CodeGenerator {}
    }

    pub fn generate_module(&self, asts: &Vec<TopAst>) -> ir::Module {
        let mut module = ir::Module::new();
        for top in asts {
            use TopAst::*;
            match &top {
                Import(_) => {}
                Function(f) => {
                    module.remember_function(f);
                }
                Variable(v) => {
                    module.remember_variable(v);
                }
                Class(_) => {}
                Trait(_) => unimplemented!(),
            }
        }
        for top in asts {
            use TopAst::*;
            match &top {
                Import(_) => {}
                Function(f) => {
                    if f.tag.is_builtin() {
                        continue;
                    }
                    let func = ir::Function::from_ast(f, None, &mut module);
                    module.push_function(func);
                }
                Variable(v) => {
                    let var = ir::Variable::new(v.name.clone(), ir::Expr::from_ast(&v.expr));
                    module.push_variable(var);
                }
                Class(c) => {
                    match c.name.as_str() {
                        // FIXME: provide a tag, e.g.
                        // ```
                        // @Codegen(Omit)
                        // class int {}
                        // ```
                        "void" | "int" | "f64" | "bool" | "_c_string" | "List" => continue,
                        _ => {}
                    }
                    module.push_type(&c.name, &c.members);

                    for member in &c.members {
                        match member {
                            ClassMember::StaticMethod(static_method) => {
                                let func = ir::Function::from_ast(
                                    static_method,
                                    Some(c.name.clone()),
                                    &mut module,
                                );
                                module.push_function(func);
                            }
                            ClassMember::Method(method) => {
                                let mut method = method.clone();
                                method.parameters.insert(
                                    0,
                                    Parameter::new("self", ParsedType::TypeName(c.name.clone())),
                                );
                                let func = ir::Function::from_ast(
                                    &method,
                                    Some(c.name.clone()),
                                    &mut module,
                                );
                                module.push_function(func);
                            }
                            _ => (),
                        }
                    }
                }
                Trait(_) => unimplemented!(),
            }
        }
        module
    }
}

#[cfg(test)]
mod tests;
