# Capítulo 1 — Criando seu primeiro device em C99

> Este capítulo é para quem nunca usou o IoTMaker. Ao final dele você terá
> criado um device chamado `stdOut`, com três blocos — `print_int`,
> `print_float` e `print_string` —, e terá usado um deles dentro de um
> projeto para gerar código-fonte C99.
>
> Tempo estimado: 20 minutos. Pré-requisito: saber o básico de C
> (escrever uma função simples).

---

## 1.1 O que é um device?

No IoTMaker, um **device** é uma caixinha gráfica que o usuário arrasta para
o **stage** (a área de trabalho da IDE) e conecta a outras caixinhas usando
fios. Cada fio carrega um valor — um número, um texto, um sinal.

Por trás de cada device existe código de verdade. Quando você é um
**especialista**, você escreve esse código uma única vez e o IoTMaker o
transforma em blocos gráficos. Depois disso, qualquer pessoa — inclusive
quem nunca programou — pode usar os seus blocos apenas ligando fios.

Neste capítulo vamos criar o device mais simples possível: três funções C
que imprimem um valor na tela — um inteiro, um número de ponto flutuante
e um texto. **Cada função vira um bloco independente** dentro do mesmo
device.

---

## 1.2 Criando o device no painel

> 📸 **CAPTURA PENDENTE:** tela do painel *Devices & Templates* com o
> dropdown *New Project* aberto, mostrando a opção *Create with wizard*.
> Sugestão de nome do arquivo: `img/c99-01-new-device.png`

1. Entre no painel de controle e abra a seção **Devices & Templates**.
2. Clique em **New Project** e escolha **Create with wizard**.
3. Dê um nome ao device. Neste capítulo usaremos `stdOut`.
4. Escolha **C** como linguagem de programação.

Ao confirmar, a IDE abre o editor de código do device, ainda vazio:

![Editor de código vazio](img/c99-02-editor-vazio.png)

Repare na barra superior. Ela vai nos acompanhar durante todo o capítulo:

| Elemento          | O que faz                                                  |
|-------------------|------------------------------------------------------------|
| **Editor**        | É onde você escreve o código C do device.                  |
| **Wizard**        | Configura o que o Parse encontrou (nomes, ícones, portas). |
| **Preview**       | Mostra como os blocos gráficos vão ficar na IDE.           |
| **Debug**         | Informações técnicas do parse, para investigar problemas.  |
| **Parse**         | Lê o seu código e descobre as funções e parâmetros.        |
| **Save**          | Grava uma versão do device (v1, v2, ...).                  |
| **Live analysis** | Quando marcado, analisa o código enquanto você digita.     |

---

## 1.3 Escrevendo o código C

Digite (ou cole) o código abaixo no **Editor**:

```c
// stdout.c
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

#include <stdio.h>

// print_int writes a single integer to standard output, followed by a
// newline. Host targets only (PC/Linux): stdout is a real stream here.
// On microcontrollers there is no stdout — the serial/UART path is used
// instead, so this function is not portable to embedded targets.
//
// Português: Escreve um inteiro na saída padrão, seguido de nova linha.
// Só para alvos host (PC/Linux), onde stdout é um stream real. Em
// microcontroladores não há stdout — usa-se a serial/UART.
void print_int(int value) {
    printf("%d\n", value);
}

// print_float writes a single floating-point number to standard output,
// followed by a newline. The %f conversion expects a double; a float
// argument is promoted to double automatically by the C varargs rules,
// so passing `value` directly is correct and portable. Host targets
// only (PC/Linux) — see print_int for the embedded-target caveat.
//
// Português: Escreve um número de ponto flutuante na saída padrão,
// seguido de nova linha. O %f espera double; um float é promovido a
// double automaticamente pelas regras de varargs do C, então passar
// `value` direto é correto. Só para alvos host (PC/Linux).
void print_float(float value) {
    printf("%f\n", value);
}

// print_string writes a NUL-terminated string to standard output,
// followed by a newline. A NULL pointer is printed as "(null)" instead
// of being passed to printf — printf("%s", NULL) is undefined behavior
// in C, so the guard keeps the block safe no matter what the maker
// wires into it. Host targets only (PC/Linux) — see print_int.
//
// Português: Escreve uma string terminada em NUL na saída padrão,
// seguida de nova linha. Ponteiro NULL é impresso como "(null)" em vez
// de ir para o printf — printf("%s", NULL) é comportamento indefinido
// em C, então a guarda mantém o bloco seguro independentemente do que
// o maker ligar nele. Só para alvos host (PC/Linux).
void print_string(const char *value) {
    if (value == NULL) {
        printf("(null)\n");
        return;
    }
    printf("%s\n", value);
}
```

> 📸 **CAPTURA PENDENTE:** editor com o código das três funções.
> Sugestão: `img/c99-03-codigo.png` (substitui a captura da versão de
> uma função)

Quatro coisas importantes acontecem nesse código, e vale a pena entender
cada uma antes de seguir:

**Cada função vira um bloco.** O arquivo tem três funções públicas —
`print_int`, `print_float` e `print_string` — então o device `stdOut`
oferece três blocos ao usuário. Um device é como uma caixa de ferramentas:
ele agrupa blocos relacionados.

**O parâmetro vira uma porta.** O parâmetro de cada função vira uma porta
de entrada no bloco correspondente — é nela que o usuário liga o fio com
o valor a ser impresso. O tipo do parâmetro define a cor do fio e quais
outras portas podem ser conectadas:

| Tipo em C      | Tipo do fio na IDE | Observação                                                                                                                                               |
|----------------|--------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `int`          | `INT`              | Número inteiro.                                                                                                                                          |
| `float`        | `FLOAT`            | Número de ponto flutuante. A largura em bits (32/64) é decidida pela placa-alvo na hora de gerar o código — você não precisa se preocupar com isso aqui. |
| `const char *` | `STRING`           | Texto. Em C, uma string é um ponteiro para caracteres; o `const` avisa que a função só lê o texto, nunca o modifica.                                     |

**O comentário vira documentação.** O bloco de comentário imediatamente
acima de cada função é capturado pelo Parse e exibido para o usuário
dentro da IDE. Escreva-o pensando em quem vai *usar* o bloco, não em quem
vai ler o código. Você pode escrever em mais de um idioma no mesmo
comentário, como no exemplo — mas a forma correta de documentar em vários
idiomas é criar arquivos de manual, que veremos na seção 1.8.

**Código defensivo protege o usuário.** Repare na guarda de `NULL` do
`print_string`: em C, passar um ponteiro nulo para `printf("%s", ...)` é
comportamento indefinido — o programa pode travar. Como o especialista não
controla o que o maker vai ligar na porta, o bloco imprime `(null)` em vez
de arriscar. Blocos defensivos são a marca de um bom especialista: o
usuário confia que a caixinha nunca vai derrubar o programa dele.

> **Dica:** este device usa `printf`, que só existe em alvos com sistema
> operacional (PC/Linux). Em um microcontrolador não há `stdout` — a saída
> é pela serial/UART. Por isso os comentários avisam que as funções não
> são portáveis para embarcados. Documentar limitações é parte do
> trabalho: o usuário do bloco confia no que você escreveu.

---

## 1.4 Parse: transformando código em blocos

Com o código pronto, clique em **Parse** (repare no lembrete no canto
direito da tela: *Click Parse to visualise*).

O Parse lê o arquivo e descobre as três funções e seus parâmetros. Em
seguida, o **Wizard** abre uma janela de configuração para cada item
encontrado. Vamos configurar o `print_int` em detalhe — depois é só
repetir o mesmo processo para `print_float` e `print_string`.

A primeira janela é a da função:

![Wizard da função print_int](img/c99-04-wizard-funcao.png)

| Campo                | O que preencher                                                                                                                            |
|----------------------|--------------------------------------------------------------------------------------------------------------------------------------------|
| **ID**               | Identificador interno, vem do nome da função. Não se altera.                                                                               |
| **Label**            | O nome que aparece escrito no bloco, no stage. Pode ser mais amigável que o nome da função.                                                |
| **Icon**             | O desenho que aparece no topo do bloco. Clique em um ícone da grade ou digite o nome dele (são ícones FontAwesome, como `bolt` ou `gear`). |
| **Callback handler** | Deixe em **— Not a callback handler —**. Callbacks são um recurso avançado, tratado em outro capítulo.                                     |

Escolha um ícone que ajude o usuário a reconhecer o bloco de longe —
para funções de impressão, algo como um terminal ou uma seta de saída
funciona bem. Você pode usar o mesmo ícone para os três blocos ou variar
(por exemplo, `hashtag` para o inteiro, `percent` para o float,
`quote-right` para a string). Depois clique em **Save** na janela.

> O botão **Add help**, entre Cancel e Save, cria o manual do device.
> Vamos usá-lo na seção 1.8 — por enquanto, siga adiante.

---

## 1.5 Configurando as portas

Na sequência, o Wizard abre a configuração da porta de cada função. O
título da janela mostra o caminho completo — por exemplo,
**Port · print_int · input · value** (porta *value*, de entrada, da
função *print_int*).

![Escolhendo o tipo de conexão da porta](img/c99-05-porta-connection.png)

O campo mais importante aqui é **Connection**, e ele tem duas opções:

- **Optional — port may be left unwired**: o usuário pode deixar a porta
  sem fio. Use quando a função tem um valor padrão razoável ou quando o
  parâmetro é um ajuste fino.
- **Mandatory — port must be wired**: o usuário é obrigado a ligar um fio
  nessa porta. Se não ligar, a IDE acusa erro antes de gerar código.

Para as três portas deste device, escolha **Mandatory**: imprimir "nada"
não faz sentido, então cada porta precisa sempre receber um valor.

O campo **Comment** também é obrigatório. Ele é a descrição da porta:
aparece quando o usuário passa o mouse sobre o pino na IDE e vira
comentário no código gerado. Uma frase curta basta — escreva o que a
porta *carrega*, por exemplo:

| Porta                  | Comment sugerido                |
|------------------------|---------------------------------|
| `print_int · value`    | `Integer value to print`        |
| `print_float · value`  | `Floating-point value to print` |
| `print_string · value` | `Text to print`                 |

![Porta configurada com comentário](img/c99-06-porta-comentario.png)

Clique em **Save** na janela de cada porta. Ao final, você terá
configurado três funções e três portas.

---

## 1.6 Conferindo no Preview

Abra a aba **Preview**. Ela mostra os blocos exatamente como eles vão
aparecer no stage:

> 📸 **CAPTURA PENDENTE:** Preview exibindo os três blocos
> (`print_int`, `print_float`, `print_string`).
> Sugestão: `img/c99-07-preview.png` (substitui a captura da versão de
> uma função)

Confira, em cada bloco:

1. **O ícone e o label** estão como você configurou no Wizard.
2. **A porta `value`** aparece com o tipo correto ao lado (`int`,
   `float32`, `string`).
3. **A cor do pino** indica a obrigatoriedade — a legenda abaixo dos
   blocos mostra: pino **verde** = *mandatory* (obrigatório), pino
   **azul vazado** = *optional* (opcional).

Passe o mouse sobre os pinos (*Hover pins for details*) para ver os
comentários que você escreveu na seção anterior.

---

## 1.7 Salvando a primeira versão

De volta à barra superior, clique em **Save**. O device ganha a versão
**v1**, que passa a aparecer ao lado do nome na barra de navegação.

A partir da segunda versão, o botão **Diff** permite comparar o que
mudou entre versões — útil quando você evolui o device sem quebrar os
projetos de quem já o usa.

---

## 1.8 Escrevendo o manual do device (recomendado!)

Seu device já funciona, mas ainda falta o mais importante para quem vai
usá-lo: **o manual**.

Se você não escrever um manual, a IDE mostra ao usuário apenas o
comentário do código-fonte — que é melhor que nada, mas mistura idiomas e
detalhes técnicos que o usuário final não precisa ver:

![Documentação vinda só do comentário do código](img/c99-09-doc-sem-manual.png)

Compare com um device nativo da IDE, como o **Add**, que tem manual de
verdade — descrição amigável, tabela de portas e até uma dica de uso:

![Manual do device Add, como referência](img/c99-08-manual-add.png)

É esse o padrão que queremos. Para criar o manual:

1. Volte ao **Editor**, rode **Parse** (o gerenciador de ajuda precisa do
   resultado do parse para saber quais arquivos criar).
2. Abra o **Wizard**, entre na janela de uma função e clique em
   **Add help**.
3. O gerenciador de arquivos de ajuda abre com a janela de criação já
   preparada. Escolha:
	- **O que documentar:** *main menu text* cria o texto geral do device
	  (o que aparece no menu da IDE); o nome de uma função (ex.:
	  `print_int`) cria a documentação daquele bloco específico.
	- **O idioma:** `en`, `pt-br`, `es`, `fr`, `de`, `zh-cn` ou `ja`.
4. Escreva o conteúdo em Markdown e salve.
5. Repita para cada bloco e cada idioma que quiser cobrir.

Os arquivos seguem a convenção `<nome>.<idioma>.md`. Para o `stdOut`
completo, em inglês e português, o conjunto fica assim:

```
readme.en.md          readme.pt-br.md          ← texto do menu
print_int.en.md       print_int.pt-br.md       ← manual do bloco int
print_float.en.md     print_float.pt-br.md     ← manual do bloco float
print_string.en.md    print_string.pt-br.md    ← manual do bloco string
```

A IDE escolhe automaticamente o arquivo no idioma do usuário.

> 📸 **CAPTURA PENDENTE:** gerenciador de arquivos de ajuda aberto com a
> janela de criação (escolha de basename e idioma).
> Sugestão: `img/c99-10-add-help.png`

**Estrutura sugerida para o manual de um bloco** (a mesma usada pelos
devices nativos):

```markdown
# print_int — Imprimir inteiro

Imprime um número inteiro na tela, seguido de quebra de linha.

## Portas

| Porta | Direção | Tipo | Descrição              |
|-------|---------|------|------------------------|
| value | Entrada | int  | Valor a ser impresso   |

## Dica

Este device funciona apenas em alvos PC/Linux. Em microcontroladores,
use um device de saída serial.
```

---

## 1.9 Usando o device em um projeto

Agora vamos usar o `stdOut` de verdade. Crie (ou abra) um projeto do
tipo **C99** na IDE — repare no indicador `C99` no canto superior direito
do stage.

Abra o **Menu** (o hexágono no canto do stage) e navegue até
**My Items** na barra lateral. É ali que ficam os devices que *você*
criou. Dentro do `stdOut` você encontra os três blocos:

> 📸 **CAPTURA PENDENTE:** My Items listando `print_int`, `print_float`
> e `print_string`. Sugestão: `img/c99-09b-my-items.png` (substitui a
> captura da versão de uma função)

Clique em `print_int` e depois em **+ Place on stage**. O bloco aparece
no stage, pronto para receber fios.

Para testar, monte este pequeno programa: "pegue o elemento de índice 3
de uma lista de números e imprima-o na tela". Você vai precisar de mais
três devices, todos nativos da IDE:

- **Const → Array Int** (`constArrayInt`): a lista `{0,1,2,3,4,5,6,...}`.
- **Const → Int** (`constInt`): a constante `3`, o índice desejado.
- **Index** (`indexInt`): recebe a lista e o índice, devolve o elemento.

Ligue os fios assim:

1. Saída do `constArrayInt_0` → primeira entrada do `indexInt_0` (a lista).
2. Saída do `constInt_0` → segunda entrada do `indexInt_0` (o índice).
3. Saída do `indexInt_0` → porta **value** do seu `print_int_1`.

![Programa completo no stage](img/c99-11-stage-completo.png)

Repare que o fio só "aceita" conectar portas de tipos compatíveis — a
lista (`[]INT`, fio mais grosso) só entra na porta de lista, e o `INT`
comum só entra nas portas `INT`. A IDE impede ligações erradas antes
mesmo de você gerar o código.

> **Experimente:** troque o `print_int` pelo `print_string` e tente ligar
> a saída `INT` do index na porta dele. A IDE recusa a conexão — os tipos
> não batem. É assim que o IoTMaker evita, em tempo de montagem, os erros
> que em C só apareceriam na compilação (ou pior, em tempo de execução).

---

## 1.10 Gerando o código-fonte

> 📸 **CAPTURA PENDENTE:** (1) menu Export com a opção de código C;
> (2) seletor de placa (board picker); (3) trecho do código C gerado,
> mostrando a função `print_int` incluída no projeto.
> Sugestões: `img/c99-12-export.png`, `img/c99-13-board-picker.png`,
> `img/c99-14-codigo-gerado.png`

Com o programa montado, abra o **Menu → Export** e escolha a exportação
de código C. A IDE pergunta para qual placa você quer gerar o código —
para este teste, escolha **PC** para compilar e rodar no seu computador
(lembre-se: `printf` não existe em microcontroladores).

O IoTMaker gera um projeto C99 completo, em formato zip, contendo:

- o código do seu device `stdOut`, exatamente como você escreveu;
- o código dos devices nativos usados (constantes, index);
- um `main` que liga tudo na ordem certa, seguindo os fios do stage.

Descompacte, compile e rode. Se tudo deu certo, o programa imprime `3`
na tela — o elemento de índice 3 da lista `{0,1,2,3,...}`.

**Parabéns: você criou seu primeiro device C99 e o usou para gerar um
programa de verdade.** 🎉

---

## 1.11 Erros comuns

| Sintoma                                                    | Causa provável                                                             | Solução                                                                                      |
|------------------------------------------------------------|----------------------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| O Parse não encontra uma função                            | A função não está com assinatura completa ou o arquivo tem erro de sintaxe | Confira o código no Editor; o **Live analysis** sublinha problemas enquanto você digita      |
| O botão **Add help** avisa "Run Parse first"               | O gerenciador de ajuda precisa do resultado do parse                       | Rode **Parse** e tente novamente                                                             |
| Os blocos não aparecem em **My Items**                     | O device não foi salvo (**Save**) após o Parse                             | Volte ao editor do device e salve a versão                                                   |
| A IDE acusa porta obrigatória sem fio                      | Uma porta **Mandatory** ficou desconectada no stage                        | Ligue um fio na porta indicada ou mude a porta para **Optional** na próxima versão do device |
| O fio não conecta em uma porta                             | Os tipos não são compatíveis (ex.: `INT` em porta `STRING`)                | Use um bloco de conversão ou a porta do tipo certo — no `stdOut`, cada tipo tem seu bloco    |
| A documentação do bloco aparece "misturada" (dois idiomas) | O device não tem arquivos de manual; a IDE caiu no comentário do código    | Crie os arquivos de manual (seção 1.8), um por idioma                                        |

---

*Próximo capítulo: criando devices C99 com portas de saída e múltiplos
parâmetros.*
